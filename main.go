package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	awsclient "ec2manager/aws"
	"ec2manager/config"
	"ec2manager/db"
	"ec2manager/handlers"
	"ec2manager/middleware"
	"ec2manager/models"
	"ec2manager/scheduler"
)

func main() {
	cfg := config.Load()

	// ── Logging ───────────────────────────────────────────────
	logFile, err := os.OpenFile(cfg.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: cannot open log file %s: %v\n", cfg.LogFile, err)
		os.Exit(1)
	}
	defer logFile.Close()

	multiWriter := io.MultiWriter(os.Stdout, logFile)
	logger := slog.New(slog.NewJSONHandler(multiWriter, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	// ── Database ──────────────────────────────────────────────
	database, err := db.Initialize(cfg)
	if err != nil {
		logger.Error("database initialisation failed", "error", err)
		os.Exit(1)
	}
	defer database.Close()
	logger.Info("database ready", "path", cfg.DBPath)

	// ── AWS ───────────────────────────────────────────────────
	ec2Client, err := awsclient.NewEC2Client(context.Background(), cfg.AWSRegion)
	if err != nil {
		logger.Error("AWS client initialisation failed", "error", err)
		os.Exit(1)
	}
	logger.Info("AWS EC2 client ready", "region", cfg.AWSRegion)

	// ── Rate Limiter ──────────────────────────────────────────
	// Allow 5 failed login attempts per 15-minute window before locking for 15 min.
	rl := middleware.NewRateLimiter(5, 15*time.Minute, 15*time.Minute)

	// ── Handlers ──────────────────────────────────────────────
	h, err := handlers.New(database, ec2Client, logger, rl)
	if err != nil {
		logger.Error("handler initialisation failed", "error", err)
		os.Exit(1)
	}

	// ── Router ────────────────────────────────────────────────
	mux := http.NewServeMux()

	// Static assets.
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))

	// Public routes (no auth).
	mux.HandleFunc("POST /api/heartbeat", h.Heartbeat)

	// Auth routes.
	mux.HandleFunc("GET /login", h.LoginPage)
	mux.HandleFunc("POST /login", h.Login)
	mux.HandleFunc("POST /logout", middleware.Auth(database)(h.Logout))

	// Default: redirect "/" → "/login".
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	})

	// Developer routes (session auth required).
	authMW := middleware.Auth(database)
	mux.HandleFunc("GET /dashboard", authMW(h.Dashboard))
	mux.HandleFunc("POST /start-instance", authMW(h.StartInstance))
	mux.HandleFunc("POST /stop-instance", authMW(h.StopInstance))

	// Admin routes (session auth + admin role required).
	adminMW := func(fn http.HandlerFunc) http.HandlerFunc {
		return authMW(middleware.AdminOnly(fn))
	}
	mux.HandleFunc("GET /admin", adminMW(h.AdminDashboard))
	mux.HandleFunc("POST /admin/users", adminMW(h.AddUser))
	mux.HandleFunc("POST /admin/users/{id}/assign", adminMW(h.AssignInstance))
	mux.HandleFunc("POST /admin/users/{id}/reset-pin", adminMW(h.ResetPIN))
	mux.HandleFunc("POST /admin/users/{id}/delete", adminMW(h.DeleteUser))

	// ── Auto-stop Scheduler ───────────────────────────────────
	sched := scheduler.New(database, ec2Client, logger)
	sched.Start()
	defer sched.Stop()

	// ── Periodic Session Cleanup ──────────────────────────────
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := models.CleanExpiredSessions(database); err != nil {
				logger.Warn("session cleanup error", "error", err)
			}
		}
	}()

	// ── HTTP Server ───────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      requestLogger(logger)(mux),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown on SIGINT / SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		logger.Info("server listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-quit
	logger.Info("shutdown signal received, draining connections…")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
	logger.Info("server stopped cleanly")
}

// requestLogger is a minimal structured HTTP access-log middleware.
func requestLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &captureWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)
			logger.Info("http",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rw.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"remote_addr", middleware.GetClientIP(r),
			)
		})
	}
}

// captureWriter wraps http.ResponseWriter to capture the status code for logging.
type captureWriter struct {
	http.ResponseWriter
	status int
}

func (cw *captureWriter) WriteHeader(code int) {
	cw.status = code
	cw.ResponseWriter.WriteHeader(code)
}

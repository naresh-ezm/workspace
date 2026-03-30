package main

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/filesystem"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"

	awsclient "ec2manager/aws"
	"ec2manager/config"
	"ec2manager/db"
	"ec2manager/handlers"
	"ec2manager/middleware"
	"ec2manager/models"
	"ec2manager/scheduler"
)

//go:embed all:web/build
var svelteFS embed.FS

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
	slogger := slog.New(slog.NewJSONHandler(multiWriter, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(slogger)

	// ── Database ──────────────────────────────────────────────
	database, err := db.Initialize(cfg)
	if err != nil {
		slogger.Error("database initialisation failed", "error", err)
		os.Exit(1)
	}
	defer database.Close()
	slogger.Info("database ready", "path", cfg.DBPath)

	// ── AWS ───────────────────────────────────────────────────
	ec2Client, err := awsclient.NewEC2Client(context.Background(), cfg.AWSRegion)
	if err != nil {
		slogger.Error("AWS client initialisation failed", "error", err)
		os.Exit(1)
	}
	slogger.Info("AWS EC2 client ready", "region", cfg.AWSRegion)

	// ── Rate Limiter ──────────────────────────────────────────
	rl := middleware.NewRateLimiter(5, 15*time.Minute, 15*time.Minute)

	// ── Fiber App ────────────────────────────────────────────
	app := fiber.New(fiber.Config{
		ProxyHeader:       fiber.HeaderXForwardedFor,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		ErrorHandler: errorHandler(slogger),
	})

	// ── Global Middleware ────────────────────────────────────
	app.Use(recover.New())
	app.Use(logger.New(logger.Config{
		Format: `{"time":"${time}","method":"${method}","path":"${path}","status":${status},"latency":"${latency}","ip":"${ip}"}` + "\n",
		Output: multiWriter,
	}))

	// ── Handlers ─────────────────────────────────────────────
	h, err := handlers.New(database, ec2Client, slogger, rl, cfg)
	if err != nil {
		slogger.Error("handler initialisation failed", "error", err)
		os.Exit(1)
	}

	// ── Heartbeat (called by EC2 instances, not the frontend) ───
	app.Post("/api/heartbeat", h.Heartbeat)

	// ── JSON API Routes (used by the Svelte frontend) ────────
	apiAuthMW := h.APIAuthMW()
	apiAdminMW := handlers.APIAdminOnlyMW

	// Public API
	app.Post("/api/login", h.APILogin)
	app.Post("/api/mfa/verify", h.APIMFAVerify)

	// Authenticated API
	app.Get("/api/me", apiAuthMW, h.APIMe)
	app.Post("/api/logout", apiAuthMW, h.APILogout)
	app.Get("/api/mfa/setup", apiAuthMW, h.APIMFASetupGet)
	app.Post("/api/mfa/setup", apiAuthMW, h.APIMFASetupPost)
	app.Post("/api/mfa/disable", apiAuthMW, h.APIMFADisable)
	app.Get("/api/dashboard", apiAuthMW, h.APIDashboard)
	app.Post("/api/start-instance", apiAuthMW, h.APIStartInstance)
	app.Post("/api/stop-instance", apiAuthMW, h.APIStopInstance)

	// Admin API
	apiAdmin := app.Group("/api/admin", apiAuthMW, apiAdminMW)
	apiAdmin.Get("/", h.APIAdminDashboard)
	apiAdmin.Get("/app-logs", h.AppLogs)
	apiAdmin.Post("/users", h.APIAddUser)
	apiAdmin.Post("/users/:id/assign", h.APIAssignInstance)
	apiAdmin.Post("/users/:id/provision", h.APIProvisionWorkspace)
	apiAdmin.Post("/users/:id/reset-pin", h.APIResetPIN)
	apiAdmin.Post("/users/:id/reset-mfa", h.APIResetMFA)
	apiAdmin.Post("/users/:id/delete", h.APIDeleteUser)

	// ── Svelte SPA ───────────────────────────────────────────
	// Embedded at compile time from web/build; must come after all API routes.
	stripped, _ := fs.Sub(svelteFS, "web/build")
	app.Use("/", filesystem.New(filesystem.Config{
		Root:         http.FS(stripped),
		Index:        "index.html",
		NotFoundFile: "index.html", // SPA fallback for client-side routing
	}))

	// ── Auto-stop Scheduler ───────────────────────────────────
	sched := scheduler.New(database, ec2Client, slogger)
	sched.Start()
	defer sched.Stop()

	// ── Periodic Session Cleanup ──────────────────────────────
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if err := models.CleanExpiredSessions(database); err != nil {
				slogger.Warn("session cleanup error", "error", err)
			}
			if err := models.CleanExpiredChallenges(database); err != nil {
				slogger.Warn("mfa challenge cleanup error", "error", err)
			}
		}
	}()

	// ── Graceful Shutdown ────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slogger.Info("server listening", "addr", ":"+cfg.Port)
		if err := app.Listen(":" + cfg.Port); err != nil {
			slogger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-quit
	slogger.Info("shutdown signal received, draining connections…")

	if err := app.ShutdownWithTimeout(15 * time.Second); err != nil {
		slogger.Error("graceful shutdown failed", "error", err)
	}
	slogger.Info("server stopped cleanly")
}

func errorHandler(logger *slog.Logger) fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		code := fiber.StatusInternalServerError
		if e, ok := err.(*fiber.Error); ok {
			code = e.Code
		}
		logger.Error("unhandled error", "status", code, "path", c.Path(), "error", err)
		return c.Status(code).SendString(err.Error())
	}
}

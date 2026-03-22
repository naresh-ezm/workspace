package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/template/html/v2"

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

	// ── Template Engine ───────────────────────────────────────
	engine := html.New("./templates", ".html")
	// engine.Reload(true) // uncomment for hot-reload during development

	// ── Fiber App ────────────────────────────────────────────
	app := fiber.New(fiber.Config{
		Views:             engine,
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
	h, err := handlers.New(database, ec2Client, slogger, rl)
	if err != nil {
		slogger.Error("handler initialisation failed", "error", err)
		os.Exit(1)
	}

	// ── Static Files ─────────────────────────────────────────
	app.Static("/static", "./static")

	// ── Public Routes ────────────────────────────────────────
	app.Post("/api/heartbeat", h.Heartbeat)
	app.Get("/login", h.LoginPage)
	app.Post("/login", h.Login)
	app.Get("/", func(c *fiber.Ctx) error {
		return c.Redirect("/login", fiber.StatusSeeOther)
	})

	// ── Auth Routes ───────────────────────────────────────────
	authMW := middleware.Auth(database)
	app.Post("/logout", authMW, h.Logout)

	// ── Developer Routes ─────────────────────────────────────
	app.Get("/dashboard", authMW, h.Dashboard)
	app.Post("/start-instance", authMW, h.StartInstance)
	app.Post("/stop-instance", authMW, h.StopInstance)

	// ── Admin Routes ─────────────────────────────────────────
	adminMW := middleware.AdminOnly
	admin := app.Group("/admin", authMW, adminMW)
	admin.Get("/", h.AdminDashboard)
	admin.Post("/users", h.AddUser)
	admin.Post("/users/:id/assign", h.AssignInstance)
	admin.Post("/users/:id/reset-pin", h.ResetPIN)
	admin.Post("/users/:id/delete", h.DeleteUser)

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

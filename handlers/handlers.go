package handlers

import (
	"database/sql"
	"log/slog"

	"github.com/gofiber/fiber/v2"

	awsclient "ec2manager/aws"
	"ec2manager/config"
	"ec2manager/middleware"
	"ec2manager/models"
)

// Handler holds shared dependencies for all HTTP handlers.
type Handler struct {
	DB     *sql.DB
	EC2    *awsclient.EC2Client
	Logger *slog.Logger
	RL     *middleware.RateLimiter
	Config *config.Config
}

// New builds a Handler.
func New(
	db *sql.DB,
	ec2Client *awsclient.EC2Client,
	logger *slog.Logger,
	rl *middleware.RateLimiter,
	cfg *config.Config,
) (*Handler, error) {
	return &Handler{
		DB:     db,
		EC2:    ec2Client,
		Logger: logger,
		RL:     rl,
		Config: cfg,
	}, nil
}

// ──────────────────────────────────────────────────────────────
// Shared template data structures
// ──────────────────────────────────────────────────────────────

// BaseData is embedded in every page's data struct so the layout template can
// always render the navigation bar.
type BaseData struct {
	CurrentUser *models.User
}

// LogEntry enriches a Log row with the author's username.
type LogEntry struct {
	*models.Log
	Username string
}

// ──────────────────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────────────────

// render executes the named page template inside the shared layout.
func (h *Handler) render(c *fiber.Ctx, page string, data any) error {
	return c.Render(page, data, "layout")
}

// logAction persists an audit log entry, logging any write error.
func (h *Handler) logAction(userID *int64, action models.ActionType, instanceID, metadata string) {
	if err := models.CreateLog(h.DB, userID, action, instanceID, metadata); err != nil {
		h.Logger.Error("failed to write audit log", "action", action, "error", err)
	}
}

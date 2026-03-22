package handlers

import (
	"database/sql"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"

	awsclient "ec2manager/aws"
	"ec2manager/middleware"
	"ec2manager/models"
)

// Handler holds shared dependencies for all HTTP handlers.
type Handler struct {
	DB     *sql.DB
	EC2    *awsclient.EC2Client
	Logger *slog.Logger
	RL     *middleware.RateLimiter
	Tmpls  *Templates
}

// Templates holds pre-parsed template sets for each page.
type Templates struct {
	Login     *template.Template
	Dashboard *template.Template
	Admin     *template.Template
}

// LoadTemplates parses all page templates against the shared layout.
func LoadTemplates() (*Templates, error) {
	parse := func(pages ...string) (*template.Template, error) {
		files := append([]string{"templates/layout.html"}, pages...)
		return template.ParseFiles(files...)
	}

	login, err := parse("templates/login.html")
	if err != nil {
		return nil, fmt.Errorf("login template: %w", err)
	}
	dashboard, err := parse("templates/dashboard.html")
	if err != nil {
		return nil, fmt.Errorf("dashboard template: %w", err)
	}
	admin, err := parse("templates/admin.html")
	if err != nil {
		return nil, fmt.Errorf("admin template: %w", err)
	}

	return &Templates{
		Login:     login,
		Dashboard: dashboard,
		Admin:     admin,
	}, nil
}

// New builds a Handler, loading templates from disk.
func New(
	db *sql.DB,
	ec2Client *awsclient.EC2Client,
	logger *slog.Logger,
	rl *middleware.RateLimiter,
) (*Handler, error) {
	tmpls, err := LoadTemplates()
	if err != nil {
		return nil, err
	}
	return &Handler{
		DB:     db,
		EC2:    ec2Client,
		Logger: logger,
		RL:     rl,
		Tmpls:  tmpls,
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

// render executes the named template set, writing the result to w.
func (h *Handler) render(w http.ResponseWriter, tmpl *template.Template, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tmpl.ExecuteTemplate(w, "layout", data); err != nil {
		h.Logger.Error("template execution failed", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// logAction persists an audit log entry, logging any write error.
func (h *Handler) logAction(userID *int64, action models.ActionType, instanceID, metadata string) {
	if err := models.CreateLog(h.DB, userID, action, instanceID, metadata); err != nil {
		h.Logger.Error("failed to write audit log", "action", action, "error", err)
	}
}

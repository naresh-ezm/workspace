package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"time"

	"ec2manager/middleware"
	"ec2manager/models"

	"golang.org/x/crypto/bcrypt"
)

type loginPageData struct {
	BaseData
	Error string
}

// LoginPage renders the login form (GET /login).
func (h *Handler) LoginPage(w http.ResponseWriter, r *http.Request) {
	// If already logged in, redirect appropriately.
	if cookie, err := r.Cookie(middleware.SessionCookieName); err == nil {
		if sess, err := models.GetSessionByToken(h.DB, cookie.Value); err == nil {
			if user, err := models.GetUserByID(h.DB, sess.UserID); err == nil {
				if user.Role == models.RoleAdmin {
					http.Redirect(w, r, "/admin", http.StatusSeeOther)
				} else {
					http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
				}
				return
			}
		}
	}
	h.render(w, h.Tmpls.Login, loginPageData{})
}

// Login handles PIN authentication (POST /login).
func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	ip := middleware.GetClientIP(r)

	if !h.RL.IsAllowed(ip) {
		h.render(w, h.Tmpls.Login, loginPageData{
			Error: "Too many failed attempts. Please wait 15 minutes before trying again.",
		})
		return
	}

	username := sanitize(r.FormValue("username"))
	pin := r.FormValue("pin")

	renderError := func(msg string) {
		h.RL.RecordFailedAttempt(ip)
		h.logAction(nil, models.ActionLoginFail, "", fmt.Sprintf(`{"username":"%s","ip":"%s"}`, username, ip))
		h.Logger.Warn("failed login attempt", "username", username, "ip", ip)
		h.render(w, h.Tmpls.Login, loginPageData{Error: msg})
	}

	if username == "" || pin == "" {
		renderError("Username and PIN are required.")
		return
	}

	user, err := models.GetUserByUsername(h.DB, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			renderError("Invalid username or PIN.")
			return
		}
		h.Logger.Error("db error during login", "error", err)
		renderError("An internal error occurred. Please try again.")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PINHash), []byte(pin)); err != nil {
		renderError("Invalid username or PIN.")
		return
	}

	// Success – reset rate-limiter and create session.
	h.RL.Reset(ip)

	token, err := generateToken()
	if err != nil {
		h.Logger.Error("failed to generate session token", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	expiresAt := time.Now().Add(8 * time.Hour)
	if err := models.CreateSession(h.DB, user.ID, token, expiresAt); err != nil {
		h.Logger.Error("failed to create session", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     middleware.SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	h.logAction(&user.ID, models.ActionLogin, "", fmt.Sprintf(`{"ip":"%s"}`, ip))
	h.Logger.Info("user logged in", "username", user.Username, "role", user.Role, "ip", ip)

	if user.Role == models.RoleAdmin {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
	}
}

// Logout invalidates the session and clears the cookie (POST /logout).
func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	user := middleware.GetUser(r)

	if cookie, err := r.Cookie(middleware.SessionCookieName); err == nil {
		_ = models.DeleteSession(h.DB, cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     middleware.SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})

	if user != nil {
		h.logAction(&user.ID, models.ActionLogout, "", "")
		h.Logger.Info("user logged out", "username", user.Username)
	}

	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

// ──────────────────────────────────────────────────────────────
// Internal helpers
// ──────────────────────────────────────────────────────────────

func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// sanitize strips leading/trailing whitespace and limits length to prevent
// excessively long inputs reaching the database.
func sanitize(s string) string {
	if len(s) > 256 {
		s = s[:256]
	}
	// Trim common whitespace.
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 {
		last := s[len(s)-1]
		if last == ' ' || last == '\t' || last == '\n' || last == '\r' {
			s = s[:len(s)-1]
		} else {
			break
		}
	}
	return s
}

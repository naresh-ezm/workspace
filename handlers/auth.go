package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/pquerna/otp/totp"

	"ec2manager/middleware"
	"ec2manager/models"

	"golang.org/x/crypto/bcrypt"
)

type loginPageData struct {
	BaseData
	Error string
}

// LoginPage renders the login form (GET /login).
func (h *Handler) LoginPage(c *fiber.Ctx) error {
	// If already logged in, redirect appropriately.
	token := c.Cookies(middleware.SessionCookieName)
	if token != "" {
		if sess, err := models.GetSessionByToken(h.DB, token); err == nil {
			if user, err := models.GetUserByID(h.DB, sess.UserID); err == nil {
				if user.Role == models.RoleAdmin {
					return c.Redirect("/admin", fiber.StatusSeeOther)
				}
				return c.Redirect("/dashboard", fiber.StatusSeeOther)
			}
		}
	}
	return h.render(c, "login", loginPageData{})
}

// Login handles PIN authentication (POST /login).
func (h *Handler) Login(c *fiber.Ctx) error {
	ip := c.IP()

	if !h.RL.IsAllowed(ip) {
		return h.render(c, "login", loginPageData{
			Error: "Too many failed attempts. Please wait 15 minutes before trying again.",
		})
	}

	username := sanitize(c.FormValue("username"))
	pin := c.FormValue("pin")

	renderError := func(msg string) error {
		h.RL.RecordFailedAttempt(ip)
		h.logAction(nil, models.ActionLoginFail, "", fmt.Sprintf(`{"username":"%s","ip":"%s"}`, username, ip))
		h.Logger.Warn("failed login attempt", "username", username, "ip", ip)
		return h.render(c, "login", loginPageData{Error: msg})
	}

	if username == "" || pin == "" {
		return renderError("Username and PIN are required.")
	}

	user, err := models.GetUserByUsername(h.DB, username)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return renderError("Invalid username or PIN.")
		}
		h.Logger.Error("db error during login", "error", err)
		return renderError("An internal error occurred. Please try again.")
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PINHash), []byte(pin)); err != nil {
		return renderError("Invalid username or PIN.")
	}

	// Success – reset rate-limiter.
	h.RL.Reset(ip)

	// If the user has MFA enabled, issue a short-lived challenge and redirect
	// to the TOTP verification page instead of creating a session immediately.
	if user.TOTPEnabled {
		challengeToken, err := generateToken()
		if err != nil {
			h.Logger.Error("failed to generate MFA challenge token", "error", err)
			return fiber.ErrInternalServerError
		}
		expiresAt := time.Now().Add(5 * time.Minute)
		if err := models.CreateMFAChallenge(h.DB, user.ID, challengeToken, expiresAt); err != nil {
			h.Logger.Error("failed to create MFA challenge", "error", err)
			return fiber.ErrInternalServerError
		}
		c.Cookie(&fiber.Cookie{
			Name:     "mfa_challenge",
			Value:    challengeToken,
			Path:     "/mfa/verify",
			Expires:  expiresAt,
			HTTPOnly: true,
			SameSite: "Lax",
		})
		h.Logger.Info("MFA challenge issued", "username", user.Username, "ip", ip)
		return c.Redirect("/mfa/verify", fiber.StatusSeeOther)
	}

	token, err := generateToken()
	if err != nil {
		h.Logger.Error("failed to generate session token", "error", err)
		return fiber.ErrInternalServerError
	}

	expiresAt := time.Now().Add(8 * time.Hour)
	if err := models.CreateSession(h.DB, user.ID, token, expiresAt); err != nil {
		h.Logger.Error("failed to create session", "error", err)
		return fiber.ErrInternalServerError
	}

	c.Cookie(&fiber.Cookie{
		Name:     middleware.SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expiresAt,
		HTTPOnly: true,
		SameSite: "Lax",
	})

	h.logAction(&user.ID, models.ActionLogin, "", fmt.Sprintf(`{"ip":"%s"}`, ip))
	h.Logger.Info("user logged in", "username", user.Username, "role", user.Role, "ip", ip)

	if user.Role == models.RoleAdmin {
		return c.Redirect("/admin", fiber.StatusSeeOther)
	}
	return c.Redirect("/dashboard", fiber.StatusSeeOther)
}

// Logout invalidates the session and clears the cookie (POST /logout).
func (h *Handler) Logout(c *fiber.Ctx) error {
	user := middleware.GetUser(c)

	token := c.Cookies(middleware.SessionCookieName)
	if token != "" {
		_ = models.DeleteSession(h.DB, token)
	}

	c.Cookie(&fiber.Cookie{
		Name:     middleware.SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Now().Add(-time.Hour),
		MaxAge:   -1,
		HTTPOnly: true,
	})

	if user != nil {
		h.logAction(&user.ID, models.ActionLogout, "", "")
		h.Logger.Info("user logged out", "username", user.Username)
	}

	return c.Redirect("/login", fiber.StatusSeeOther)
}

// MFAVerifyPage renders the TOTP code entry form (GET /mfa/verify).
func (h *Handler) MFAVerifyPage(c *fiber.Ctx) error {
	if c.Cookies("mfa_challenge") == "" {
		return c.Redirect("/login", fiber.StatusSeeOther)
	}
	return h.render(c, "mfa_verify", struct {
		BaseData
		Error string
	}{})
}

// MFAVerify validates the submitted TOTP code and, on success, creates a full
// session (POST /mfa/verify).
func (h *Handler) MFAVerify(c *fiber.Ctx) error {
	ip := c.IP()

	if !h.RL.IsAllowed(ip) {
		return h.render(c, "mfa_verify", struct {
			BaseData
			Error string
		}{Error: "Too many failed attempts. Please wait before trying again."})
	}

	challengeToken := c.Cookies("mfa_challenge")
	if challengeToken == "" {
		return c.Redirect("/login", fiber.StatusSeeOther)
	}

	challenge, err := models.GetMFAChallenge(h.DB, challengeToken)
	if err != nil {
		return c.Redirect("/login?error=mfa_expired", fiber.StatusSeeOther)
	}

	user, err := models.GetUserByID(h.DB, challenge.UserID)
	if err != nil {
		return c.Redirect("/login", fiber.StatusSeeOther)
	}

	if !totp.Validate(c.FormValue("code"), user.TOTPSecret.String) {
		h.RL.RecordFailedAttempt(ip)
		h.Logger.Warn("invalid TOTP code", "user_id", user.ID, "ip", ip)
		return h.render(c, "mfa_verify", struct {
			BaseData
			Error string
		}{Error: "Invalid code. Please try again."})
	}

	_ = models.DeleteMFAChallenge(h.DB, challengeToken)
	h.RL.Reset(ip)

	// Clear the MFA challenge cookie.
	c.Cookie(&fiber.Cookie{
		Name:     "mfa_challenge",
		Value:    "",
		Path:     "/mfa/verify",
		Expires:  time.Now().Add(-time.Hour),
		MaxAge:   -1,
		HTTPOnly: true,
	})

	sessionToken, err := generateToken()
	if err != nil {
		return fiber.ErrInternalServerError
	}
	expiresAt := time.Now().Add(time.Duration(h.Config.SessionDuration) * time.Hour)
	if err := models.CreateSession(h.DB, user.ID, sessionToken, expiresAt); err != nil {
		return fiber.ErrInternalServerError
	}
	c.Cookie(&fiber.Cookie{
		Name:     middleware.SessionCookieName,
		Value:    sessionToken,
		Path:     "/",
		Expires:  expiresAt,
		HTTPOnly: true,
		SameSite: "Lax",
	})

	h.logAction(&user.ID, models.ActionLogin, "", fmt.Sprintf(`{"ip":"%s","mfa":true}`, ip))
	h.Logger.Info("user logged in with MFA", "username", user.Username, "ip", ip)

	if user.Role == models.RoleAdmin {
		return c.Redirect("/admin", fiber.StatusSeeOther)
	}
	return c.Redirect("/dashboard", fiber.StatusSeeOther)
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

// sanitize strips leading/trailing whitespace and limits input length.
func sanitize(s string) string {
	if len(s) > 256 {
		s = s[:256]
	}
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

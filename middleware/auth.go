package middleware

import (
	"database/sql"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"

	"ec2manager/models"
)

const (
	UserLocalsKey     = "user"
	SessionCookieName = "session_token"
)

// ──────────────────────────────────────────────────────────────
// Rate Limiter
// ──────────────────────────────────────────────────────────────

// RateLimiter is a simple, in-memory, per-IP login rate limiter.
type RateLimiter struct {
	mu           sync.Mutex
	records      map[string]*attemptRecord
	maxAttempts  int
	window       time.Duration
	lockDuration time.Duration
}

type attemptRecord struct {
	count       int
	firstSeen   time.Time
	lockedUntil time.Time
}

// NewRateLimiter creates a limiter that allows maxAttempts within window before
// locking an IP for lockDuration.
func NewRateLimiter(maxAttempts int, window, lockDuration time.Duration) *RateLimiter {
	rl := &RateLimiter{
		records:      make(map[string]*attemptRecord),
		maxAttempts:  maxAttempts,
		window:       window,
		lockDuration: lockDuration,
	}
	go rl.periodicCleanup()
	return rl
}

// IsAllowed returns false when the IP is locked out.
func (rl *RateLimiter) IsAllowed(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rec, ok := rl.records[ip]
	if !ok {
		return true
	}

	now := time.Now()
	if now.Before(rec.lockedUntil) {
		return false
	}
	if now.After(rec.firstSeen.Add(rl.window)) {
		delete(rl.records, ip)
		return true
	}
	return rec.count < rl.maxAttempts
}

// RecordFailedAttempt increments the failure counter and locks if the limit is hit.
func (rl *RateLimiter) RecordFailedAttempt(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	rec, ok := rl.records[ip]
	if !ok || now.After(rec.firstSeen.Add(rl.window)) {
		rl.records[ip] = &attemptRecord{count: 1, firstSeen: now}
		return
	}
	rec.count++
	if rec.count >= rl.maxAttempts {
		rec.lockedUntil = now.Add(rl.lockDuration)
	}
}

// Reset clears the record for an IP (called on successful login).
func (rl *RateLimiter) Reset(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.records, ip)
}

func (rl *RateLimiter) periodicCleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	for range ticker.C {
		rl.mu.Lock()
		now := time.Now()
		for ip, rec := range rl.records {
			if now.After(rec.lockedUntil) && now.After(rec.firstSeen.Add(rl.window)) {
				delete(rl.records, ip)
			}
		}
		rl.mu.Unlock()
	}
}

// ──────────────────────────────────────────────────────────────
// Fiber Middleware
// ──────────────────────────────────────────────────────────────

// Auth validates the session cookie and stores the authenticated user in
// c.Locals for downstream handlers.  On failure it redirects to /login.
func Auth(db *sql.DB) fiber.Handler {
	return func(c *fiber.Ctx) error {
		token := c.Cookies(SessionCookieName)
		if token == "" {
			return c.Redirect("/login", fiber.StatusSeeOther)
		}

		session, err := models.GetSessionByToken(db, token)
		if err != nil {
			clearSessionCookie(c)
			return c.Redirect("/login", fiber.StatusSeeOther)
		}

		user, err := models.GetUserByID(db, session.UserID)
		if err != nil {
			clearSessionCookie(c)
			return c.Redirect("/login", fiber.StatusSeeOther)
		}

		c.Locals(UserLocalsKey, user)
		return c.Next()
	}
}

// AdminOnly enforces that the authenticated user holds the admin role.
// Must be used after Auth.
func AdminOnly(c *fiber.Ctx) error {
	user := GetUser(c)
	if user == nil || user.Role != models.RoleAdmin {
		return fiber.NewError(fiber.StatusForbidden, "Forbidden – admin access required")
	}
	return c.Next()
}

// GetUser extracts the authenticated user stored by the Auth middleware.
func GetUser(c *fiber.Ctx) *models.User {
	user, _ := c.Locals(UserLocalsKey).(*models.User)
	return user
}

func clearSessionCookie(c *fiber.Ctx) {
	c.Cookie(&fiber.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		Expires:  time.Now().Add(-time.Hour),
		MaxAge:   -1,
		HTTPOnly: true,
	})
}

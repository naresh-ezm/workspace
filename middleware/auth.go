package middleware

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"sync"
	"time"

	"ec2manager/models"
)

// Context key type to avoid collisions with third-party packages.
type contextKey string

const (
	UserContextKey    contextKey = "user"
	SessionCookieName            = "session_token"
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
		return false // still locked
	}
	if now.After(rec.firstSeen.Add(rl.window)) {
		delete(rl.records, ip) // window expired, reset
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
// Middleware
// ──────────────────────────────────────────────────────────────

// Auth validates the session cookie and injects the authenticated user into the
// request context.  On failure it redirects to /login.
func Auth(db *sql.DB) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(SessionCookieName)
			if err != nil {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			session, err := models.GetSessionByToken(db, cookie.Value)
			if err != nil {
				clearSessionCookie(w)
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			user, err := models.GetUserByID(db, session.UserID)
			if err != nil {
				clearSessionCookie(w)
				http.Redirect(w, r, "/login", http.StatusSeeOther)
				return
			}

			ctx := context.WithValue(r.Context(), UserContextKey, user)
			next(w, r.WithContext(ctx))
		}
	}
}

// AdminOnly enforces that the authenticated user holds the admin role.
// Must be used after the Auth middleware.
func AdminOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r)
		if user == nil || user.Role != models.RoleAdmin {
			http.Error(w, "Forbidden – admin access required", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

// GetUser extracts the authenticated user from the request context.
func GetUser(r *http.Request) *models.User {
	user, _ := r.Context().Value(UserContextKey).(*models.User)
	return user
}

// GetClientIP extracts the real client IP, honouring X-Forwarded-For when
// the app sits behind a reverse-proxy.
func GetClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For may contain a comma-separated list; take the first.
		parts := strings.SplitN(xff, ",", 2)
		return strings.TrimSpace(parts[0])
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}
	// Strip port from RemoteAddr (e.g. "1.2.3.4:56789" → "1.2.3.4").
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
}

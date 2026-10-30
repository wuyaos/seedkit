// Package auth implements single-user password login with cookie sessions
// and a fixed-window login rate limit.
package auth

import (
	"crypto/rand"
	"encoding/hex"
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

const cookieName = "seedkit_session"

type Manager struct {
	mu        sync.Mutex
	username  string
	hash      string
	ttl       time.Duration
	sessions  map[string]time.Time
	failures  map[string]*failure
	secureCk  bool
}

type failure struct {
	count int
	until time.Time
}

func New(username, hash string, ttl time.Duration) *Manager {
	return &Manager{
		username: username, hash: hash, ttl: ttl,
		sessions: map[string]time.Time{},
		failures: map[string]*failure{},
	}
}

// NeedsSetup reports whether no credentials are configured yet.
func (m *Manager) NeedsSetup() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.hash == ""
}

func (m *Manager) SetCredentials(username, password string) (hash string, err error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	m.mu.Lock()
	m.username, m.hash = username, string(b)
	m.sessions = map[string]time.Time{}
	m.mu.Unlock()
	return string(b), nil
}

// Login verifies credentials with rate limiting keyed by client IP.
// After 5 failures the IP is locked for 1 minute, doubling each round.
func (m *Manager) Login(r *http.Request, username, password string) (token string, ok bool, retryAfter time.Duration) {
	ip := clientIP(r)
	m.mu.Lock()
	f := m.failures[ip]
	if f != nil && time.Now().Before(f.until) {
		d := time.Until(f.until)
		m.mu.Unlock()
		return "", false, d
	}
	user, hash := m.username, m.hash
	m.mu.Unlock()

	valid := hash != "" && username == user &&
		bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil

	m.mu.Lock()
	defer m.mu.Unlock()
	if !valid {
		if f == nil {
			f = &failure{}
			m.failures[ip] = f
		}
		f.count++
		if f.count >= 5 {
			lock := time.Minute << uint(min(f.count-5, 6))
			f.until = time.Now().Add(lock)
			return "", false, lock
		}
		return "", false, 0
	}
	delete(m.failures, ip)
	tok := randomToken()
	m.sessions[tok] = time.Now().Add(m.ttl)
	return tok, true, 0
}

func (m *Manager) Logout(token string) {
	m.mu.Lock()
	delete(m.sessions, token)
	m.mu.Unlock()
}

func (m *Manager) Valid(token string) bool {
	if token == "" {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	exp, ok := m.sessions[token]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(m.sessions, token)
		return false
	}
	return true
}

func (m *Manager) SetCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/", HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil,
		Expires:  time.Now().Add(m.ttl),
	})
}

func ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
}

func TokenFrom(r *http.Request) string {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// Middleware rejects unauthenticated API requests with 401.
func (m *Manager) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !m.Valid(TokenFrom(r)) {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

package admin

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// sessions is a simple in-memory bearer-token store with expiry.
type sessions struct {
	mu  sync.Mutex
	m   map[string]time.Time
	ttl time.Duration
}

func newSessions(ttl time.Duration) *sessions {
	return &sessions{m: make(map[string]time.Time), ttl: ttl}
}

func (s *sessions) create() string {
	tok := randToken()
	s.mu.Lock()
	s.m[tok] = time.Now().Add(s.ttl)
	s.mu.Unlock()
	return tok
}

func (s *sessions) valid(tok string) bool {
	if tok == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	exp, ok := s.m[tok]
	if !ok {
		return false
	}
	if time.Now().After(exp) {
		delete(s.m, tok)
		return false
	}
	// sliding expiry
	s.m[tok] = time.Now().Add(s.ttl)
	return true
}

func (s *sessions) revoke(tok string) {
	s.mu.Lock()
	delete(s.m, tok)
	s.mu.Unlock()
}

func randToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// hashPassword returns a bcrypt hash of the given password.
func hashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

// checkPassword verifies pw against a bcrypt hash.
func checkPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

// bearer extracts the token from the Authorization header.
func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(h) > len(p) && h[:len(p)] == p {
		return h[len(p):]
	}
	return ""
}

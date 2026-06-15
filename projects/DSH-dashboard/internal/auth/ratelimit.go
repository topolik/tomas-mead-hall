package auth

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

type loginBucket struct {
	count    int
	lockedAt *time.Time
}

var (
	loginMu      sync.Mutex
	loginBuckets = map[string]*loginBucket{}
)

const (
	maxAttempts  = 5
	lockDuration = 10 * time.Minute
)

// CheckLoginRate returns true if the IP is allowed to attempt login.
func CheckLoginRate(r *http.Request) bool {
	ip := clientIP(r)
	loginMu.Lock()
	defer loginMu.Unlock()

	b := loginBuckets[ip]
	if b == nil {
		loginBuckets[ip] = &loginBucket{}
		return true
	}
	if b.lockedAt != nil {
		if time.Since(*b.lockedAt) > lockDuration {
			delete(loginBuckets, ip)
			return true
		}
		return false
	}
	return b.count < maxAttempts
}

// RecordLoginFailure increments the failure count for an IP.
func RecordLoginFailure(r *http.Request) {
	ip := clientIP(r)
	loginMu.Lock()
	defer loginMu.Unlock()

	b := loginBuckets[ip]
	if b == nil {
		b = &loginBucket{}
		loginBuckets[ip] = b
	}
	b.count++
	if b.count >= maxAttempts {
		now := time.Now()
		b.lockedAt = &now
	}
}

// RecordLoginSuccess clears the failure record for an IP.
func RecordLoginSuccess(r *http.Request) {
	ip := clientIP(r)
	loginMu.Lock()
	defer loginMu.Unlock()
	delete(loginBuckets, ip)
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.SplitN(xff, ",", 2)[0]
	}
	addr := r.RemoteAddr
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[:i]
	}
	return addr
}

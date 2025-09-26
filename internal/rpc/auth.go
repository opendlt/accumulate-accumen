package rpc

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// APIKeyMiddleware validates API keys from X-API-Key header
type APIKeyMiddleware struct {
	allowedKeys map[string]bool
	mu          sync.RWMutex
}

// NewAPIKeyMiddleware creates a new API key middleware
func NewAPIKeyMiddleware(keys []string) *APIKeyMiddleware {
	allowedKeys := make(map[string]bool)
	for _, key := range keys {
		if key != "" {
			allowedKeys[key] = true
		}
	}

	return &APIKeyMiddleware{
		allowedKeys: allowedKeys,
	}
}

// UpdateKeys updates the allowed API keys
func (m *APIKeyMiddleware) UpdateKeys(keys []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.allowedKeys = make(map[string]bool)
	for _, key := range keys {
		if key != "" {
			m.allowedKeys[key] = true
		}
	}
}

// Middleware returns the API key validation middleware
func (m *APIKeyMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip API key check for health endpoint
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		// Check if API keys are configured
		m.mu.RLock()
		hasKeys := len(m.allowedKeys) > 0
		m.mu.RUnlock()

		if !hasKeys {
			// No API keys configured - allow all requests
			next.ServeHTTP(w, r)
			return
		}

		// Get API key from header
		apiKey := r.Header.Get("X-API-Key")
		if apiKey == "" {
			writeAuthError(w, "Missing X-API-Key header")
			return
		}

		// Validate API key
		m.mu.RLock()
		valid := m.allowedKeys[apiKey]
		m.mu.RUnlock()

		if !valid {
			writeAuthError(w, "Invalid API key")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// RateLimiter implements token bucket rate limiting per IP
type RateLimiter struct {
	limiters map[string]*rate.Limiter
	mu       sync.RWMutex
	rps      int
	burst    int
	cleanup  *time.Ticker
	ctx      context.Context
	cancel   context.CancelFunc
}

// NewRateLimiter creates a new rate limiter
func NewRateLimiter(rps, burst int) *RateLimiter {
	ctx, cancel := context.WithCancel(context.Background())
	rl := &RateLimiter{
		limiters: make(map[string]*rate.Limiter),
		rps:      rps,
		burst:    burst,
		cleanup:  time.NewTicker(time.Minute),
		ctx:      ctx,
		cancel:   cancel,
	}

	// Start cleanup goroutine
	go rl.cleanupLoop()

	return rl
}

// Close stops the rate limiter and cleanup goroutine
func (rl *RateLimiter) Close() {
	rl.cancel()
	rl.cleanup.Stop()
}

// getLimiter returns the rate limiter for the given IP
func (rl *RateLimiter) getLimiter(ip string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limiter, exists := rl.limiters[ip]
	if !exists {
		limiter = rate.NewLimiter(rate.Limit(rl.rps), rl.burst)
		rl.limiters[ip] = limiter
	}

	return limiter
}

// cleanupLoop removes inactive rate limiters
func (rl *RateLimiter) cleanupLoop() {
	for {
		select {
		case <-rl.ctx.Done():
			return
		case <-rl.cleanup.C:
			rl.mu.Lock()
			for ip, limiter := range rl.limiters {
				// Remove limiters that haven't been used in the last minute
				if limiter.Allow() {
					// If we can get a token, put it back and keep the limiter
					continue
				}
				// If we can't get a token and the limiter is at capacity,
				// it might be inactive - remove it
				delete(rl.limiters, ip)
			}
			rl.mu.Unlock()
		}
	}
}

// Middleware returns the rate limiting middleware
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip rate limiting for health endpoint
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		// Skip if rate limiting is disabled
		if rl.rps <= 0 {
			next.ServeHTTP(w, r)
			return
		}

		// Get client IP
		ip := getClientIP(r)
		limiter := rl.getLimiter(ip)

		if !limiter.Allow() {
			writeRateLimitError(w, "Rate limit exceeded")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// CORSMiddleware handles Cross-Origin Resource Sharing
type CORSMiddleware struct {
	allowedOrigins map[string]bool
	allowAll       bool
	mu             sync.RWMutex
}

// NewCORSMiddleware creates a new CORS middleware
func NewCORSMiddleware(origins []string) *CORSMiddleware {
	allowedOrigins := make(map[string]bool)
	allowAll := false

	for _, origin := range origins {
		if origin == "*" {
			allowAll = true
			break
		}
		if origin != "" {
			allowedOrigins[origin] = true
		}
	}

	return &CORSMiddleware{
		allowedOrigins: allowedOrigins,
		allowAll:       allowAll,
	}
}

// UpdateOrigins updates the allowed origins
func (m *CORSMiddleware) UpdateOrigins(origins []string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.allowedOrigins = make(map[string]bool)
	m.allowAll = false

	for _, origin := range origins {
		if origin == "*" {
			m.allowAll = true
			break
		}
		if origin != "" {
			m.allowedOrigins[origin] = true
		}
	}
}

// Middleware returns the CORS middleware
func (m *CORSMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Set CORS headers
		m.mu.RLock()
		if m.allowAll {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else if origin != "" && m.allowedOrigins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		}
		m.mu.RUnlock()

		w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-API-Key, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400")

		// Handle preflight requests
		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// TLSConfig loads TLS configuration from certificate and key files
type TLSConfig struct {
	CertFile string
	KeyFile  string
}

// LoadTLSConfig loads TLS configuration from files
func LoadTLSConfig(certFile, keyFile string) (*tls.Config, error) {
	if certFile == "" || keyFile == "" {
		return nil, nil // No TLS configuration
	}

	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load TLS certificate: %w", err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305,
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
		},
	}, nil
}

// SecurityHeadersMiddleware adds security headers
func SecurityHeadersMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Security headers
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

		// Content Security Policy for JSON API
		w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'none'; object-src 'none'")

		next.ServeHTTP(w, r)
	})
}

// LoggingMiddleware logs HTTP requests
func LoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap the response writer to capture status code
		ww := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

		next.ServeHTTP(ww, r)

		duration := time.Since(start)

		// Log request (in production, use structured logging)
		fmt.Printf("[%s] %s %s %d %v %s\n",
			start.Format(time.RFC3339),
			r.Method,
			r.URL.Path,
			ww.statusCode,
			duration,
			getClientIP(r),
		)
	})
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

// getClientIP extracts the client IP from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (for load balancers/proxies)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Take the first IP from the comma-separated list
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// writeAuthError writes an authentication error response
func writeAuthError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)

	response := map[string]interface{}{
		"error": map[string]interface{}{
			"code":    -32401,
			"message": "Unauthorized: " + message,
		},
	}

	// Write JSON response (simplified - in production use proper JSON encoding)
	fmt.Fprintf(w, `{"error":{"code":-32401,"message":"Unauthorized: %s"}}`, message)
}

// writeRateLimitError writes a rate limit error response
func writeRateLimitError(w http.ResponseWriter, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", "60")
	w.WriteHeader(http.StatusTooManyRequests)

	// Write JSON response
	fmt.Fprintf(w, `{"error":{"code":-32429,"message":"Rate limit exceeded: %s"}}`, message)
}
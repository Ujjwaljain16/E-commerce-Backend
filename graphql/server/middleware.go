package server

import (
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"github.com/Ujjwaljain16/E-commerce-Backend/graphql/graph"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/metrics"
)

const (
	serviceName  = "graphql-gateway"
	maxBodyBytes = 1 << 20
)

// Authenticate attaches the caller's claims to the request context when it
// carries a valid bearer token. Anything else continues unauthenticated, and
// resolvers that need a user reject it themselves.
func Authenticate(tokens graph.TokenValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token, ok := bearerToken(r.Header.Get("Authorization")); ok {
				if claims, err := tokens.ValidateAccessToken(token); err == nil {
					r = r.WithContext(graph.WithClaims(r.Context(), claims))
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func bearerToken(header string) (string, bool) {
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

// LimitBody rejects request bodies larger than maxBodyBytes.
func LimitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ContentLength > maxBodyBytes {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "request body too large", "PAYLOAD_TOO_LARGE")
			return
		}
		// Chunked bodies have no Content-Length, so cap the reader as well.
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// CORS answers cross-origin requests from the allowed origins. With no
// allowed origins it adds nothing, which leaves browsers' same-origin policy in place.
func CORS(allowed []string) func(http.Handler) http.Handler {
	allowAll := false
	set := make(map[string]bool, len(allowed))
	for _, o := range allowed {
		if o == "*" {
			allowAll = true
		}
		set[o] = true
	}
	return func(next http.Handler) http.Handler {
		if len(set) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			w.Header().Add("Vary", "Origin")
			if origin == "" || (!allowAll && !set[origin]) {
				next.ServeHTTP(w, r)
				return
			}
			h := w.Header()
			if allowAll {
				h.Set("Access-Control-Allow-Origin", "*")
			} else {
				h.Set("Access-Control-Allow-Origin", origin)
			}
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RateLimiter is a per-client-IP token bucket.
type RateLimiter struct {
	limit      rate.Limit
	burst      int
	trustProxy bool
	idleTTL    time.Duration
	now        func() time.Time

	mu        sync.Mutex
	clients   map[string]*client
	lastSweep time.Time
}

type client struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewRateLimiter allows rps requests per second per client with the given burst.
func NewRateLimiter(rps float64, burst int, trustProxy bool) *RateLimiter {
	return &RateLimiter{
		limit:      rate.Limit(rps),
		burst:      burst,
		trustProxy: trustProxy,
		idleTTL:    3 * time.Minute,
		now:        time.Now,
		clients:    make(map[string]*client),
	}
}

// Allow reports whether a request from ip may proceed.
func (l *RateLimiter) Allow(ip string) bool {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	// Sweeping inline on a timer keeps memory bounded without a background goroutine.
	if now.Sub(l.lastSweep) >= l.idleTTL {
		for key, c := range l.clients {
			if now.Sub(c.lastSeen) >= l.idleTTL {
				delete(l.clients, key)
			}
		}
		l.lastSweep = now
	}

	c, ok := l.clients[ip]
	if !ok {
		c = &client{limiter: rate.NewLimiter(l.limit, l.burst)}
		l.clients[ip] = c
	}
	c.lastSeen = now
	return c.limiter.AllowN(now, 1)
}

func (l *RateLimiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.clients)
}

// Middleware rejects clients over their limit with 429.
func (l *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !l.Allow(clientIP(r, l.trustProxy)) {
			w.Header().Set("Retry-After", "1")
			writeJSONError(w, http.StatusTooManyRequests, "rate limit exceeded", "RATE_LIMITED")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request, trustProxy bool) string {
	if trustProxy {
		// The right-most entry is the one our own proxy appended; anything to
		// its left came from the client and can be forged.
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeJSONError(w http.ResponseWriter, status int, message, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(`{"errors":[{"message":` + strconv.Quote(message) + `,"extensions":{"code":` + strconv.Quote(code) + `}}]}`))
}

// statusRecorder captures the response status for metrics.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Instrument records request count and latency under a fixed endpoint label,
// so arbitrary request paths can't blow up metric cardinality.
func Instrument(endpoint string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		method := methodLabel(r.Method)
		metrics.HTTPRequestsTotal.WithLabelValues(serviceName, endpoint, method, strconv.Itoa(rec.status)).Inc()
		metrics.HTTPRequestDuration.WithLabelValues(serviceName, endpoint, method).Observe(time.Since(start).Seconds())
	})
}

func methodLabel(method string) string {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodOptions, http.MethodHead:
		return method
	default:
		return "OTHER"
	}
}

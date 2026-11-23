package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"

	accountpb "github.com/Ujjwaljain16/E-commerce-Backend/account/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/graphql/graph"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/auth"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
)

const testSecret = "test-secret"

func newTokens() *auth.TokenService {
	return auth.NewTokenService(testSecret, 15*time.Minute, 7*24*time.Hour)
}

func TestAuthenticate(t *testing.T) {
	tokens := newTokens()
	access, refresh, err := tokens.GenerateTokenPair("alice", "alice@example.com", "USER")
	if err != nil {
		t.Fatal(err)
	}
	expired, err := auth.NewTokenService(testSecret, -time.Minute, time.Hour).GenerateAccessToken("alice", "alice@example.com", "USER")
	if err != nil {
		t.Fatal(err)
	}
	forged, err := auth.NewTokenService("other-secret", time.Minute, time.Hour).GenerateAccessToken("alice", "alice@example.com", "ADMIN")
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name   string
		header string
		want   string
	}{
		{"valid access token", "Bearer " + access, "alice"},
		{"lowercase scheme", "bearer " + access, "alice"},
		{"no header", "", ""},
		{"refresh token", "Bearer " + refresh, ""},
		{"expired", "Bearer " + expired, ""},
		{"wrong secret", "Bearer " + forged, ""},
		{"not bearer", "Basic " + access, ""},
		{"garbage", "Bearer not-a-jwt", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got string
			reached := false
			h := Authenticate(tokens)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				reached = true
				if c, ok := graph.ClaimsFromContext(r.Context()); ok {
					got = c.UserID
				}
			}))
			req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			h.ServeHTTP(httptest.NewRecorder(), req)
			if !reached {
				t.Fatal("request should continue even without valid auth")
			}
			if got != tc.want {
				t.Fatalf("user = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestRateLimiterReturns429(t *testing.T) {
	limiter := NewRateLimiter(1, 2, false)
	now := time.Unix(1_700_000_000, 0)
	limiter.now = func() time.Time { return now }
	h := limiter.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))

	send := func(remote string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
		req.RemoteAddr = remote
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	for i := 0; i < 2; i++ {
		if rec := send("10.0.0.1:1234"); rec.Code != http.StatusOK {
			t.Fatalf("request %d: status %d", i, rec.Code)
		}
	}
	rec := send("10.0.0.1:5678")
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `"RATE_LIMITED"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
	if rec := send("10.0.0.2:1234"); rec.Code != http.StatusOK {
		t.Fatalf("other client was limited: %d", rec.Code)
	}

	now = now.Add(time.Second)
	if rec := send("10.0.0.1:1234"); rec.Code != http.StatusOK {
		t.Fatalf("token should refill after a second, got %d", rec.Code)
	}
}

func TestRateLimiterEvictsIdleClients(t *testing.T) {
	limiter := NewRateLimiter(1, 1, false)
	now := time.Unix(1_700_000_000, 0)
	limiter.now = func() time.Time { return now }

	limiter.Allow("a")
	limiter.Allow("b")
	if limiter.size() != 2 {
		t.Fatalf("size = %d", limiter.size())
	}
	now = now.Add(limiter.idleTTL)
	limiter.Allow("c")
	if limiter.size() != 1 {
		t.Fatalf("idle clients not evicted, size = %d", limiter.size())
	}
}

func TestClientIP(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	req.RemoteAddr = "192.0.2.10:4000"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 198.51.100.7")

	if got := clientIP(req, false); got != "192.0.2.10" {
		t.Fatalf("untrusted proxy: got %q", got)
	}
	if got := clientIP(req, true); got != "198.51.100.7" {
		t.Fatalf("trusted proxy: got %q", got)
	}
}

func TestLimitBody(t *testing.T) {
	var readErr error
	h := LimitBody(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, readErr = io.ReadAll(r.Body)
	}))

	big := strings.Repeat("a", maxBodyBytes+1)
	req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(big))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", rec.Code)
	}

	// Without a Content-Length the reader itself has to stop at the limit.
	req = httptest.NewRequest(http.MethodPost, "/graphql", io.NopCloser(strings.NewReader(big)))
	req.ContentLength = -1
	h.ServeHTTP(httptest.NewRecorder(), req)
	var maxErr *http.MaxBytesError
	if !errors.As(readErr, &maxErr) {
		t.Fatalf("read error = %v, want MaxBytesError", readErr)
	}

	req = httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"{ __typename }"}`))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if readErr != nil {
		t.Fatalf("small body rejected: %v", readErr)
	}
}

func TestCORS(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})

	preflight := func(h http.Handler, origin string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodOptions, "/graphql", nil)
		req.Header.Set("Origin", origin)
		req.Header.Set("Access-Control-Request-Method", "POST")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	h := CORS([]string{"https://shop.example.com"})(next)
	rec := preflight(h, "https://shop.example.com")
	if rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") != "https://shop.example.com" {
		t.Fatalf("allowed origin: status %d, headers %v", rec.Code, rec.Header())
	}
	if rec := preflight(h, "https://evil.example.com"); rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("disallowed origin got CORS headers")
	}
	if rec := preflight(CORS(nil)(next), "https://shop.example.com"); rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("CORS headers sent with no allowed origins configured")
	}
}

func TestLoadConfig(t *testing.T) {
	env := map[string]string{}
	getenv := func(k string) string { return env[k] }

	if _, err := LoadConfig(getenv); err == nil {
		t.Fatal("missing JWT_SECRET should fail")
	}

	env["JWT_SECRET"] = "s"
	cfg, err := LoadConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Port != "8000" || cfg.RateLimitRPS != 20 || cfg.RateLimitBurst != 40 || cfg.ComplexityLimit != 200 ||
		cfg.GRPCTimeout != 5*time.Second || cfg.PlaygroundEnabled || cfg.TrustProxy || cfg.CORSAllowedOrigins != nil ||
		cfg.OrderAddr != "localhost:50053" {
		t.Fatalf("defaults = %+v", cfg)
	}

	env["CORS_ALLOWED_ORIGINS"] = " https://a.example.com, ,https://b.example.com "
	env["RATE_LIMIT_RPS"] = "abc"
	if _, err := LoadConfig(getenv); err == nil {
		t.Fatal("bad RATE_LIMIT_RPS should fail")
	}
	env["RATE_LIMIT_RPS"] = "5"
	cfg, err = LoadConfig(getenv)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.CORSAllowedOrigins) != 2 || cfg.CORSAllowedOrigins[1] != "https://b.example.com" {
		t.Fatalf("origins = %q", cfg.CORSAllowedOrigins)
	}
}

type meAccounts struct {
	accountpb.AccountServiceClient
}

func (meAccounts) GetProfile(_ context.Context, in *accountpb.GetProfileRequest, _ ...grpc.CallOption) (*accountpb.GetProfileResponse, error) {
	return &accountpb.GetProfileResponse{User: &accountpb.User{Id: in.UserId, Email: "alice@example.com", Role: "USER"}}, nil
}

func newTestRouter(t *testing.T, ready func() error) http.Handler {
	t.Helper()
	cfg, err := LoadConfig(func(k string) string {
		return map[string]string{"JWT_SECRET": testSecret}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	return NewRouter(RouterOptions{
		Config:   cfg,
		Resolver: &graph.Resolver{AccountSvc: meAccounts{}, Tokens: newTokens()},
		Ready:    ready,
		Logger:   logger.New("graphql-gateway-test"),
	})
}

func TestRouterEndToEnd(t *testing.T) {
	srv := httptest.NewServer(newTestRouter(t, func() error { return nil }))
	defer srv.Close()

	access, err := newTokens().GenerateAccessToken("alice", "alice@example.com", "USER")
	if err != nil {
		t.Fatal(err)
	}

	query := func(token string) string {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/graphql", strings.NewReader(`{"query":"{ me { id email } }"}`))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return string(body)
	}

	if body := query(access); !strings.Contains(body, `"id":"alice"`) {
		t.Fatalf("authenticated me: %s", body)
	}
	if body := query(""); !strings.Contains(body, `"UNAUTHENTICATED"`) {
		t.Fatalf("anonymous me: %s", body)
	}

	// Mutations must not be possible over GET.
	resp, err := http.Get(srv.URL + "/graphql?query=" + "mutation%7BdeleteAccount%7D")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK && !strings.Contains(string(body), "errors") {
		t.Fatalf("GET mutation accepted: %d %s", resp.StatusCode, body)
	}

	for path, want := range map[string]int{"/healthz": 200, "/readyz": 200, "/playground": 404} {
		resp, err := http.Get(srv.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("%s: status %d, want %d", path, resp.StatusCode, want)
		}
	}

	resp, err = http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatal(err)
	}
	metricsBody, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(metricsBody), `service="graphql-gateway"`) {
		t.Fatal("gateway HTTP metrics missing from /metrics")
	}
}

func TestReadyzReportsBackendFailure(t *testing.T) {
	h := newTestRouter(t, func() error { return errors.New("order service unreachable") })
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestBackendsReadyAndClose(t *testing.T) {
	cfg, err := LoadConfig(func(k string) string {
		return map[string]string{"JWT_SECRET": testSecret}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	b, err := DialBackends(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.Ready(); err == nil {
		t.Fatal("closed connections should not be ready")
	}
}

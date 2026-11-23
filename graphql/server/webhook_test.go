package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/Ujjwaljain16/E-commerce-Backend/graphql/graph"
	paymentpb "github.com/Ujjwaljain16/E-commerce-Backend/payment/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
)

type webhookPayments struct {
	paymentpb.PaymentServiceClient
	mu    sync.Mutex
	calls []*paymentpb.HandleProviderWebhookRequest
	err   error
}

func (f *webhookPayments) HandleProviderWebhook(_ context.Context, in *paymentpb.HandleProviderWebhookRequest, _ ...grpc.CallOption) (*paymentpb.HandleProviderWebhookResponse, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, in)
	if in.Signature == "bad" {
		return nil, status.Error(codes.InvalidArgument, "invalid signature")
	}
	if f.err != nil {
		return nil, f.err
	}
	return &paymentpb.HandleProviderWebhookResponse{}, nil
}

func newWebhookRouter(t *testing.T, env map[string]string, payments *webhookPayments) http.Handler {
	t.Helper()
	env["JWT_SECRET"] = testSecret
	cfg, err := LoadConfig(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	return NewRouter(RouterOptions{
		Config:   cfg,
		Resolver: &graph.Resolver{AccountSvc: meAccounts{}, PaymentSvc: payments, Tokens: newTokens()},
		Ready:    func() error { return nil },
		Logger:   logger.New("graphql-gateway-test"),
	})
}

func postWebhook(h http.Handler, body []byte, signature string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/webhooks/razorpay", bytes.NewReader(body))
	req.RemoteAddr = "203.0.113.7:4444"
	req.Header.Set("Content-Type", "application/json")
	if signature != "" {
		req.Header.Set("X-Razorpay-Signature", signature)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRazorpayWebhookForwardsRawBody(t *testing.T) {
	payments := &webhookPayments{}
	h := newWebhookRouter(t, map[string]string{}, payments)

	// Whitespace and key order matter: the signature covers these exact bytes.
	body := []byte("{\"event\": \"payment.captured\",\n  \"payload\":{}}")
	rec := postWebhook(h, body, "abc123")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %q", rec.Code, rec.Body.String())
	}
	if len(payments.calls) != 1 || !bytes.Equal(payments.calls[0].Body, body) || payments.calls[0].Signature != "abc123" {
		t.Fatalf("forwarded %+v", payments.calls)
	}
}

func TestRazorpayWebhookErrors(t *testing.T) {
	tests := []struct {
		name      string
		body      []byte
		signature string
		svcErr    error
		want      int
		forwarded bool
	}{
		{"bad signature", []byte(`{}`), "bad", nil, http.StatusBadRequest, true},
		{"missing signature", []byte(`{}`), "", nil, http.StatusBadRequest, false},
		{"too large", bytes.Repeat([]byte("a"), maxWebhookBytes+1), "abc", nil, http.StatusRequestEntityTooLarge, false},
		{"razorpay disabled", []byte(`{}`), "abc", status.Error(codes.FailedPrecondition, "razorpay is not enabled"), http.StatusNotFound, true},
		{"payment service down", []byte(`{}`), "abc", status.Error(codes.Unavailable, "down"), http.StatusServiceUnavailable, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			payments := &webhookPayments{err: tt.svcErr}
			rec := postWebhook(newWebhookRouter(t, map[string]string{}, payments), tt.body, tt.signature)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
			if got := len(payments.calls) > 0; got != tt.forwarded {
				t.Fatalf("forwarded = %v, want %v", got, tt.forwarded)
			}
		})
	}
}

func TestRazorpayWebhookIsNotRateLimited(t *testing.T) {
	payments := &webhookPayments{}
	h := newWebhookRouter(t, map[string]string{"RATE_LIMIT_RPS": "0.001", "RATE_LIMIT_BURST": "1"}, payments)

	for i := 0; i < 10; i++ {
		if rec := postWebhook(h, []byte(`{}`), "abc"); rec.Code != http.StatusOK {
			t.Fatalf("webhook %d: status %d", i, rec.Code)
		}
	}

	// The same client is limited on /graphql, so the limiter is really on.
	codes := make([]int, 2)
	for i := range codes {
		req := httptest.NewRequest(http.MethodPost, "/graphql", strings.NewReader(`{"query":"{ __typename }"}`))
		req.RemoteAddr = "203.0.113.7:4444"
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		codes[i] = rec.Code
	}
	if codes[1] != http.StatusTooManyRequests {
		t.Fatalf("graphql status codes = %v, want the second to be 429", codes)
	}
}

func TestDevCheckoutPageOnlyWithPlayground(t *testing.T) {
	get := func(h http.Handler) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dev/checkout", nil))
		return rec
	}

	if rec := get(newWebhookRouter(t, map[string]string{}, &webhookPayments{})); rec.Code != http.StatusNotFound {
		t.Fatalf("without playground: status %d", rec.Code)
	}

	rec := get(newWebhookRouter(t, map[string]string{"PLAYGROUND_ENABLED": "true"}, &webhookPayments{}))
	body, _ := io.ReadAll(rec.Body)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("status %d, content type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	for _, want := range []string{"https://checkout.razorpay.com/v1/checkout.js", "confirmPayment", "paymentForOrder"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("page does not mention %s", want)
		}
	}
}

func TestLoadConfigRazorpayKeyID(t *testing.T) {
	cfg, err := LoadConfig(func(k string) string {
		return map[string]string{"JWT_SECRET": "s", "RAZORPAY_KEY_ID": " rzp_test_abc "}[k]
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RazorpayKeyID != "rzp_test_abc" {
		t.Fatalf("RazorpayKeyID = %q", cfg.RazorpayKeyID)
	}
}

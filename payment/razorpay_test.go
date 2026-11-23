package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testKeyID         = "rzp_test_key"
	testKeySecret     = "test_key_secret"
	testWebhookSecret = "test_webhook_secret"
)

type refundCall struct {
	paymentID string
	amount    int64
	key       string
}

// fakeRazorpay imitates the Razorpay endpoints the client uses, with in-memory state.
type fakeRazorpay struct {
	mu       sync.Mutex
	orders   []*razorpayOrder
	payments map[string]*razorpayPayment
	refunds  []refundCall
	captures []string
	created  int
	// failWith makes every request return this status and error description.
	failWith    int
	failMessage string
	delay       time.Duration
	authOK      bool
	server      *httptest.Server
}

func newFakeRazorpay(t *testing.T) *fakeRazorpay {
	t.Helper()
	f := &fakeRazorpay{payments: map[string]*razorpayPayment{}, authOK: true}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/orders", f.createOrder)
	mux.HandleFunc("GET /v1/orders", f.listOrders)
	mux.HandleFunc("GET /v1/orders/{id}/payments", f.orderPayments)
	mux.HandleFunc("GET /v1/payments/{id}", f.fetchPayment)
	mux.HandleFunc("POST /v1/payments/{id}/capture", f.capture)
	mux.HandleFunc("POST /v1/payments/{id}/refund", f.refund)
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		delay, failWith, failMessage := f.delay, f.failWith, f.failMessage
		f.mu.Unlock()
		if delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
				return
			}
		}
		user, pass, ok := r.BasicAuth()
		if !ok || user != testKeyID || pass != testKeySecret {
			f.mu.Lock()
			f.authOK = false
			f.mu.Unlock()
			writeRazorpayError(w, http.StatusUnauthorized, "Authentication failed")
			return
		}
		if failWith != 0 {
			writeRazorpayError(w, failWith, failMessage)
			return
		}
		mux.ServeHTTP(w, r)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeRazorpay) client(t *testing.T) *Razorpay {
	t.Helper()
	rp, err := NewRazorpay(RazorpayConfig{
		KeyID: testKeyID, KeySecret: testKeySecret, WebhookSecret: testWebhookSecret,
		BaseURL: f.server.URL + "/v1", Timeout: 2 * time.Second,
	})
	require.NoError(t, err)
	return rp
}

// setFailure makes every following request fail with code and description; 0 turns it off.
func (f *fakeRazorpay) setFailure(code int, description string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failWith, f.failMessage = code, description
}

func writeRazorpayError(w http.ResponseWriter, code int, description string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"error": map[string]string{"code": "BAD_REQUEST_ERROR", "description": description},
	})
}

func writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (f *fakeRazorpay) createOrder(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Amount   int64             `json:"amount"`
		Currency string            `json:"currency"`
		Receipt  string            `json:"receipt"`
		Notes    map[string]string `json:"notes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Amount < 100 || len(req.Receipt) > 40 {
		writeRazorpayError(w, http.StatusBadRequest, "The amount must be at least INR 1.00")
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.created++
	o := &razorpayOrder{ID: fmt.Sprintf("order_test%d", f.created), Amount: req.Amount, Currency: req.Currency, Receipt: req.Receipt, Status: "created"}
	f.orders = append(f.orders, o)
	writeJSON(w, o)
}

func (f *fakeRazorpay) listOrders(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	items := []*razorpayOrder{}
	for _, o := range f.orders {
		if o.Receipt == r.URL.Query().Get("receipt") {
			items = append(items, o)
		}
	}
	writeJSON(w, map[string]interface{}{"entity": "collection", "count": len(items), "items": items})
}

func (f *fakeRazorpay) orderPayments(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	items := []*razorpayPayment{}
	for _, p := range f.payments {
		if p.OrderID == r.PathValue("id") {
			items = append(items, p)
		}
	}
	writeJSON(w, map[string]interface{}{"entity": "collection", "count": len(items), "items": items})
}

func (f *fakeRazorpay) fetchPayment(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.payments[r.PathValue("id")]
	if !ok {
		writeRazorpayError(w, http.StatusBadRequest, "The id provided does not exist")
		return
	}
	writeJSON(w, p)
}

func (f *fakeRazorpay) capture(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p, ok := f.payments[r.PathValue("id")]
	if !ok || p.Status != razorpayAuthorized {
		writeRazorpayError(w, http.StatusBadRequest, "This payment has already been captured")
		return
	}
	f.captures = append(f.captures, p.ID)
	p.Status = razorpayCaptured
	writeJSON(w, p)
}

func (f *fakeRazorpay) refund(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Amount int64 `json:"amount"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	key := r.Header.Get("X-Refund-Idempotency")
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.refunds {
		if c.key == key {
			// Same idempotency key: Razorpay returns the original refund.
			writeJSON(w, map[string]interface{}{"id": "rfnd_1", "entity": "refund", "payment_id": c.paymentID, "status": "processed"})
			return
		}
	}
	f.refunds = append(f.refunds, refundCall{paymentID: r.PathValue("id"), amount: req.Amount, key: key})
	writeJSON(w, map[string]interface{}{"id": "rfnd_1", "entity": "refund", "payment_id": r.PathValue("id"), "status": "processed"})
}

// pay records a customer payment against a Razorpay order, as Checkout would.
func (f *fakeRazorpay) pay(orderID, paymentID, status string) *razorpayPayment {
	f.mu.Lock()
	defer f.mu.Unlock()
	var amount int64
	currency := ""
	for _, o := range f.orders {
		if o.ID == orderID {
			amount, currency = o.Amount, o.Currency
		}
	}
	p := &razorpayPayment{ID: paymentID, OrderID: orderID, Amount: amount, Currency: currency, Status: status}
	if status == razorpayFailed {
		p.ErrorDescription = "Payment was declined by the bank"
	}
	f.payments[paymentID] = p
	cp := *p
	return &cp
}

func (f *fakeRazorpay) captureCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.captures...)
}

func (f *fakeRazorpay) createdCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.created
}

func (f *fakeRazorpay) refundCalls() []refundCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]refundCall(nil), f.refunds...)
}

func sign(secret, message string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

func TestNewRazorpayRequiresAllSecrets(t *testing.T) {
	_, err := NewRazorpay(RazorpayConfig{KeyID: "k"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "RAZORPAY_KEY_SECRET")
	assert.Contains(t, err.Error(), "RAZORPAY_WEBHOOK_SECRET")
	assert.NotContains(t, err.Error(), "RAZORPAY_KEY_ID")
}

func TestPaymentSignatureVerification(t *testing.T) {
	rp, err := NewRazorpay(RazorpayConfig{KeyID: testKeyID, KeySecret: testKeySecret, WebhookSecret: testWebhookSecret})
	require.NoError(t, err)

	// Fixed vector: hex(HMAC-SHA256("test_key_secret", "order_IluGWxBm9U8zJ8|pay_IH4NVgf4Dreq1l")).
	const vector = "34e54fc403561b4f9bd4df51fe30b7c8f9d614d823e82a8e4389a8669f5af0b2"
	assert.Equal(t, vector, sign(testKeySecret, "order_IluGWxBm9U8zJ8|pay_IH4NVgf4Dreq1l"))

	assert.True(t, rp.verifyPaymentSignature("order_IluGWxBm9U8zJ8", "pay_IH4NVgf4Dreq1l", vector))
	assert.False(t, rp.verifyPaymentSignature("order_other", "pay_IH4NVgf4Dreq1l", vector), "signature is bound to our order id")
	assert.False(t, rp.verifyPaymentSignature("order_IluGWxBm9U8zJ8", "pay_other", vector))
	assert.False(t, rp.verifyPaymentSignature("order_IluGWxBm9U8zJ8", "pay_IH4NVgf4Dreq1l", ""))
	assert.False(t, rp.verifyPaymentSignature("order_IluGWxBm9U8zJ8", "pay_IH4NVgf4Dreq1l", sign(testWebhookSecret, "order_IluGWxBm9U8zJ8|pay_IH4NVgf4Dreq1l")),
		"checkout signatures use the key secret, not the webhook secret")
}

func TestWebhookSignatureVerification(t *testing.T) {
	rp, err := NewRazorpay(RazorpayConfig{KeyID: testKeyID, KeySecret: testKeySecret, WebhookSecret: testWebhookSecret})
	require.NoError(t, err)

	body := []byte(`{"event":"payment.captured"}`)
	// Fixed vector: hex(HMAC-SHA256("test_webhook_secret", body)).
	const vector = "149f0921b8dab66ba5504a4de4e376f5e30470d680899c30ffda46ef7391a360"
	assert.True(t, rp.verifyWebhookSignature(body, vector))
	assert.False(t, rp.verifyWebhookSignature([]byte(`{"event": "payment.captured"}`), vector), "the raw bytes are signed, not the JSON value")
	assert.False(t, rp.verifyWebhookSignature(body, sign(testKeySecret, string(body))))
	assert.False(t, rp.verifyWebhookSignature(body, ""))
}

func TestRazorpayClientCreatesAndFindsOrders(t *testing.T) {
	fake := newFakeRazorpay(t)
	rp := fake.client(t)
	ctx := context.Background()

	o, err := rp.createOrder(ctx, testOrderID, 49900, "INR", map[string]string{"order_id": testOrderID})
	require.NoError(t, err)
	assert.Equal(t, "order_test1", o.ID)
	assert.Equal(t, int64(49900), o.Amount)
	fake.mu.Lock()
	assert.True(t, fake.authOK, "uses basic auth with key id and secret")
	fake.mu.Unlock()

	found, err := rp.findOrderByReceipt(ctx, testOrderID)
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, o.ID, found.ID)

	none, err := rp.findOrderByReceipt(ctx, "unknown")
	require.NoError(t, err)
	assert.Nil(t, none)
}

func TestRazorpayClientAPIError(t *testing.T) {
	fake := newFakeRazorpay(t)
	rp := fake.client(t)

	_, err := rp.createOrder(context.Background(), testOrderID, 50, "INR", nil)
	var apiErr *RazorpayError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusBadRequest, apiErr.StatusCode)
	assert.Equal(t, "The amount must be at least INR 1.00", apiErr.Description)
	assert.True(t, apiErr.rejected())

	fake.setFailure(http.StatusInternalServerError, "")
	_, err = rp.fetchPayment(context.Background(), "pay_1")
	require.ErrorAs(t, err, &apiErr)
	assert.False(t, apiErr.rejected(), "server errors are retried")
	assert.True(t, isProviderUnavailable(err))
}

func TestRazorpayClientRejectsBadCredentials(t *testing.T) {
	fake := newFakeRazorpay(t)
	rp, err := NewRazorpay(RazorpayConfig{KeyID: testKeyID, KeySecret: "wrong", WebhookSecret: "w", BaseURL: fake.server.URL + "/v1"})
	require.NoError(t, err)

	_, err = rp.fetchPayment(context.Background(), "pay_1")
	var apiErr *RazorpayError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
	assert.NotContains(t, err.Error(), "wrong", "the secret never appears in errors")
}

func TestRazorpayClientTimesOut(t *testing.T) {
	fake := newFakeRazorpay(t)
	fake.mu.Lock()
	fake.delay = time.Second
	fake.mu.Unlock()
	rp, err := NewRazorpay(RazorpayConfig{
		KeyID: testKeyID, KeySecret: testKeySecret, WebhookSecret: testWebhookSecret,
		BaseURL: fake.server.URL + "/v1", Timeout: 50 * time.Millisecond,
	})
	require.NoError(t, err)

	start := time.Now()
	_, err = rp.fetchPayment(context.Background(), "pay_1")
	require.Error(t, err)
	assert.Less(t, time.Since(start), 900*time.Millisecond)
	assert.True(t, isProviderUnavailable(err), "a timeout is reported as the provider being unavailable")
}

func TestRazorpayClientRefundIsIdempotent(t *testing.T) {
	fake := newFakeRazorpay(t)
	rp := fake.client(t)

	require.NoError(t, rp.refund(context.Background(), "pay_1", 49900))
	require.NoError(t, rp.refund(context.Background(), "pay_1", 49900))

	calls := fake.refundCalls()
	require.Len(t, calls, 1)
	assert.Equal(t, refundCall{paymentID: "pay_1", amount: 49900, key: "refund_pay_1"}, calls[0])
}

func TestRazorpayClientRefundAlreadyRefundedIsDone(t *testing.T) {
	fake := newFakeRazorpay(t)
	fake.setFailure(http.StatusBadRequest, "The payment has been fully refunded already")
	assert.NoError(t, fake.client(t).refund(context.Background(), "pay_1", 100))

	fake.setFailure(http.StatusBadRequest, "Your account does not have enough balance to carry out the refund operation.")
	err := fake.client(t).refund(context.Background(), "pay_1", 100)
	var apiErr *RazorpayError
	assert.True(t, errors.As(err, &apiErr))
}

package payment

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// RazorpayBaseURL is the Razorpay REST API.
const RazorpayBaseURL = "https://api.razorpay.com/v1"

// RazorpayConfig holds the credentials from the Razorpay dashboard.
type RazorpayConfig struct {
	KeyID         string
	KeySecret     string
	WebhookSecret string
	// BaseURL defaults to RazorpayBaseURL; tests point it at a fake server.
	BaseURL string
	// Timeout bounds each API call; defaults to 10s.
	Timeout time.Duration
}

// Razorpay is a minimal client for the parts of the Razorpay API the payment service uses.
type Razorpay struct {
	keyID         string
	keySecret     string
	webhookSecret string
	baseURL       string
	http          *http.Client
}

// NewRazorpay validates cfg and returns a client.
func NewRazorpay(cfg RazorpayConfig) (*Razorpay, error) {
	var missing []string
	if cfg.KeyID == "" {
		missing = append(missing, "RAZORPAY_KEY_ID")
	}
	if cfg.KeySecret == "" {
		missing = append(missing, "RAZORPAY_KEY_SECRET")
	}
	if cfg.WebhookSecret == "" {
		missing = append(missing, "RAZORPAY_WEBHOOK_SECRET")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("razorpay needs %s", strings.Join(missing, ", "))
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = RazorpayBaseURL
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	return &Razorpay{
		keyID:         cfg.KeyID,
		keySecret:     cfg.KeySecret,
		webhookSecret: cfg.WebhookSecret,
		baseURL:       strings.TrimRight(cfg.BaseURL, "/"),
		http:          &http.Client{Timeout: cfg.Timeout},
	}, nil
}

// RazorpayError is an error response from the Razorpay API.
type RazorpayError struct {
	StatusCode  int
	Code        string
	Description string
}

func (e *RazorpayError) Error() string {
	return fmt.Sprintf("razorpay: %d %s: %s", e.StatusCode, e.Code, e.Description)
}

// rejected reports whether Razorpay refused the request itself, so repeating it cannot succeed.
// Auth (401) and rate limit (429) errors are left to retries: they are fixed by an operator or by waiting.
func (e *RazorpayError) rejected() bool { return e.StatusCode == http.StatusBadRequest }

type razorpayOrder struct {
	ID       string `json:"id"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Receipt  string `json:"receipt"`
	Status   string `json:"status"`
}

type razorpayPayment struct {
	ID               string `json:"id"`
	OrderID          string `json:"order_id"`
	Amount           int64  `json:"amount"`
	Currency         string `json:"currency"`
	Status           string `json:"status"` // created, authorized, captured, refunded or failed
	ErrorDescription string `json:"error_description"`
}

// Razorpay payment statuses.
const (
	razorpayAuthorized = "authorized"
	razorpayCaptured   = "captured"
	razorpayFailed     = "failed"
)

func (r *Razorpay) createOrder(ctx context.Context, receipt string, amount int64, currency string, notes map[string]string) (*razorpayOrder, error) {
	var o razorpayOrder
	err := r.do(ctx, http.MethodPost, "/orders", map[string]interface{}{
		"amount": amount, "currency": currency, "receipt": receipt, "notes": notes,
	}, nil, &o)
	if err != nil {
		return nil, fmt.Errorf("create razorpay order: %w", err)
	}
	return &o, nil
}

// findOrderByReceipt returns the newest order created with receipt, or nil if there is none.
func (r *Razorpay) findOrderByReceipt(ctx context.Context, receipt string) (*razorpayOrder, error) {
	var list struct {
		Items []razorpayOrder `json:"items"`
	}
	if err := r.do(ctx, http.MethodGet, "/orders?receipt="+url.QueryEscape(receipt), nil, nil, &list); err != nil {
		return nil, fmt.Errorf("find razorpay order: %w", err)
	}
	for i := range list.Items {
		if list.Items[i].Receipt == receipt {
			return &list.Items[i], nil
		}
	}
	return nil, nil
}

func (r *Razorpay) fetchPayment(ctx context.Context, paymentID string) (*razorpayPayment, error) {
	var p razorpayPayment
	if err := r.do(ctx, http.MethodGet, "/payments/"+url.PathEscape(paymentID), nil, nil, &p); err != nil {
		return nil, fmt.Errorf("fetch razorpay payment: %w", err)
	}
	return &p, nil
}

func (r *Razorpay) orderPayments(ctx context.Context, orderID string) ([]razorpayPayment, error) {
	var list struct {
		Items []razorpayPayment `json:"items"`
	}
	if err := r.do(ctx, http.MethodGet, "/orders/"+url.PathEscape(orderID)+"/payments", nil, nil, &list); err != nil {
		return nil, fmt.Errorf("list razorpay order payments: %w", err)
	}
	return list.Items, nil
}

func (r *Razorpay) capture(ctx context.Context, paymentID string, amount int64, currency string) (*razorpayPayment, error) {
	var p razorpayPayment
	err := r.do(ctx, http.MethodPost, "/payments/"+url.PathEscape(paymentID)+"/capture",
		map[string]interface{}{"amount": amount, "currency": currency}, nil, &p)
	if err != nil {
		return nil, fmt.Errorf("capture razorpay payment: %w", err)
	}
	return &p, nil
}

// refund refunds amount of a captured payment. Razorpay dedupes requests that repeat the
// idempotency key with the same body, so a retried refund never pays out twice.
func (r *Razorpay) refund(ctx context.Context, paymentID string, amount int64) error {
	err := r.do(ctx, http.MethodPost, "/payments/"+url.PathEscape(paymentID)+"/refund",
		map[string]interface{}{"amount": amount},
		map[string]string{"X-Refund-Idempotency": "refund_" + paymentID}, nil)
	var apiErr *RazorpayError
	if errors.As(err, &apiErr) && strings.Contains(strings.ToLower(apiErr.Description), "fully refunded") {
		return nil
	}
	if err != nil {
		return fmt.Errorf("refund razorpay payment: %w", err)
	}
	return nil
}

// verifyPaymentSignature checks the razorpay_signature Checkout returns: the hex HMAC-SHA256 of
// "<order_id>|<payment_id>" keyed by the API key secret. orderID must be our stored order ID,
// never the one the client sent.
func (r *Razorpay) verifyPaymentSignature(orderID, paymentID, signature string) bool {
	return validHMAC(r.keySecret, []byte(orderID+"|"+paymentID), signature)
}

// verifyWebhookSignature checks X-Razorpay-Signature: the hex HMAC-SHA256 of the raw body keyed by
// the webhook secret.
func (r *Razorpay) verifyWebhookSignature(body []byte, signature string) bool {
	return validHMAC(r.webhookSecret, body, signature)
}

func validHMAC(secret string, message []byte, signature string) bool {
	if signature == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(message)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(strings.ToLower(signature)))
}

// maxResponseBytes caps what we read from Razorpay; real responses are a few KB.
const maxResponseBytes = 1 << 20

func (r *Razorpay) do(ctx context.Context, method, path string, body interface{}, headers map[string]string, out interface{}) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.SetBasicAuth(r.keyID, r.keySecret)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := r.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		apiErr := &RazorpayError{StatusCode: resp.StatusCode}
		var envelope struct {
			Error struct {
				Code        string `json:"code"`
				Description string `json:"description"`
			} `json:"error"`
		}
		if json.Unmarshal(data, &envelope) == nil {
			apiErr.Code, apiErr.Description = envelope.Error.Code, envelope.Error.Description
		}
		return apiErr
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

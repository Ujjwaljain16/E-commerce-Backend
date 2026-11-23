package payment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/events"
)

// ProviderConfig is the payment provider selected through the environment.
type ProviderConfig struct {
	Provider string // ProviderMock or ProviderRazorpay
	Razorpay RazorpayConfig
	// Timeout is how long a Razorpay checkout may stay unpaid before the payment fails.
	Timeout time.Duration
}

// LoadProviderConfig reads PAYMENT_PROVIDER (mock or razorpay, default mock), PAYMENT_TIMEOUT
// (default 30m) and, for Razorpay, RAZORPAY_KEY_ID, RAZORPAY_KEY_SECRET and RAZORPAY_WEBHOOK_SECRET,
// all of which are then required.
func LoadProviderConfig(getenv func(string) string) (ProviderConfig, error) {
	cfg := ProviderConfig{Provider: strings.ToLower(strings.TrimSpace(getenv("PAYMENT_PROVIDER")))}
	if cfg.Provider == "" {
		cfg.Provider = ProviderMock
	}
	timeout := strings.TrimSpace(getenv("PAYMENT_TIMEOUT"))
	if timeout == "" {
		timeout = "30m"
	}
	d, err := time.ParseDuration(timeout)
	if err != nil || d <= 0 {
		return ProviderConfig{}, fmt.Errorf("PAYMENT_TIMEOUT must be a positive duration like 30m")
	}
	cfg.Timeout = d

	switch cfg.Provider {
	case ProviderMock:
		return cfg, nil
	case ProviderRazorpay:
		cfg.Razorpay = RazorpayConfig{
			KeyID:         strings.TrimSpace(getenv("RAZORPAY_KEY_ID")),
			KeySecret:     strings.TrimSpace(getenv("RAZORPAY_KEY_SECRET")),
			WebhookSecret: strings.TrimSpace(getenv("RAZORPAY_WEBHOOK_SECRET")),
		}
		if _, err := NewRazorpay(cfg.Razorpay); err != nil {
			return ProviderConfig{}, fmt.Errorf("PAYMENT_PROVIDER=razorpay: %w", err)
		}
		return cfg, nil
	default:
		return ProviderConfig{}, fmt.Errorf("PAYMENT_PROVIDER must be mock or razorpay, got %q", cfg.Provider)
	}
}

// errPaymentMismatch means a Razorpay payment does not belong to the payment it was matched with.
var errPaymentMismatch = errors.New("razorpay payment does not match the stored payment")

// openCheckout makes sure a PENDING Razorpay payment has a Razorpay order the customer can pay.
// No event is published: the saga waits for the customer.
func (s *Service) openCheckout(ctx context.Context, p *Payment, created bool) (*Payment, error) {
	if p.ProviderOrderID != "" {
		return p, nil
	}
	if s.razorpay == nil {
		return nil, ErrProviderNotConfigured
	}

	var order *razorpayOrder
	var err error
	if !created {
		// An earlier attempt may have created the Razorpay order and died before saving its ID.
		if order, err = s.razorpay.findOrderByReceipt(ctx, p.OrderID); err != nil {
			return nil, err
		}
	}
	if order == nil {
		order, err = s.razorpay.createOrder(ctx, p.OrderID, p.AmountCents, p.Currency,
			map[string]string{"order_id": p.OrderID, "payment_id": p.ID})
		var apiErr *RazorpayError
		if errors.As(err, &apiErr) && apiErr.rejected() {
			return s.fail(ctx, p, "payment provider rejected the order: "+apiErr.Description)
		}
		if err != nil {
			return nil, err
		}
	}
	if order.Amount != p.AmountCents || !strings.EqualFold(order.Currency, p.Currency) {
		return s.fail(ctx, p, "payment provider order does not match the payment")
	}

	updated, err := s.repo.SetProviderOrderID(ctx, p.ID, order.ID)
	if err != nil {
		return nil, err
	}
	s.log.Info(ctx, "Razorpay order created", map[string]interface{}{
		"payment_id": p.ID, "order_id": p.OrderID, "razorpay_order_id": updated.ProviderOrderID,
	})
	return updated, nil
}

// Confirm completes a Razorpay payment from the Checkout handler's response. The signature proves
// the customer paid for our Razorpay order; the payment is then fetched (and captured if needed)
// so the stored state matches Razorpay's.
func (s *Service) Confirm(ctx context.Context, orderID, razorpayPaymentID, signature string) (*Payment, error) {
	if s.razorpay == nil {
		return nil, ErrProviderNotConfigured
	}
	if err := requireUUID("order_id", orderID); err != nil {
		return nil, err
	}
	if razorpayPaymentID == "" || len(razorpayPaymentID) > 64 {
		return nil, invalid("provider_payment_id is required")
	}
	p, err := s.repo.GetByOrderID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if p.Provider != ProviderRazorpay || p.ProviderOrderID == "" {
		return nil, ErrNotAwaitingCheckout
	}
	if !s.razorpay.verifyPaymentSignature(p.ProviderOrderID, razorpayPaymentID, signature) {
		s.log.Warn(ctx, "Rejected checkout with a bad signature", map[string]interface{}{"order_id": orderID})
		return nil, ErrInvalidSignature
	}
	if p.ProviderRef == razorpayPaymentID {
		return p, nil
	}

	rp, err := s.razorpay.fetchPayment(ctx, razorpayPaymentID)
	if err != nil {
		return nil, err
	}
	updated, err := s.settle(ctx, p, rp)
	if errors.Is(err, errPaymentMismatch) {
		return nil, invalid("payment does not belong to this order")
	}
	return updated, err
}

// razorpayWebhook is the part of a Razorpay webhook body we read. payment.* and order.paid events
// all carry the payment under payload.payment.entity.
type razorpayWebhook struct {
	Event   string `json:"event"`
	Payload struct {
		Payment struct {
			Entity razorpayPayment `json:"entity"`
		} `json:"payment"`
	} `json:"payload"`
}

// HandleWebhook verifies and applies a Razorpay webhook. Razorpay may deliver an event more than
// once and out of order; every transition is guarded by the payment's current status, so replays
// change nothing.
func (s *Service) HandleWebhook(ctx context.Context, body []byte, signature string) error {
	if s.razorpay == nil {
		return ErrProviderNotConfigured
	}
	if !s.razorpay.verifyWebhookSignature(body, signature) {
		s.log.Warn(ctx, "Rejected webhook with a bad signature", nil)
		return ErrInvalidSignature
	}
	var hook razorpayWebhook
	if err := json.Unmarshal(body, &hook); err != nil {
		s.log.Warn(ctx, "Ignoring undecodable webhook", map[string]interface{}{"error": err.Error()})
		return nil
	}
	switch hook.Event {
	case "payment.authorized", "payment.captured", "payment.failed", "order.paid":
	default:
		return nil
	}
	rp := hook.Payload.Payment.Entity
	fields := map[string]interface{}{"event": hook.Event, "razorpay_order_id": rp.OrderID, "razorpay_payment_id": rp.ID}
	if rp.OrderID == "" || rp.ID == "" {
		s.log.Warn(ctx, "Ignoring webhook without a payment", fields)
		return nil
	}

	p, err := s.repo.GetByProviderOrderID(ctx, rp.OrderID)
	if errors.Is(err, ErrPaymentNotFound) {
		// Payments made outside this service on the same Razorpay account.
		s.log.Info(ctx, "Ignoring webhook for an unknown order", fields)
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := s.settle(ctx, p, &rp); err != nil {
		if errors.Is(err, errPaymentMismatch) {
			s.log.Error(ctx, "Webhook payment does not match the stored payment", fields)
			return nil
		}
		return err
	}
	return nil
}

// settle applies the state of a Razorpay payment to our payment p.
func (s *Service) settle(ctx context.Context, p *Payment, rp *razorpayPayment) (*Payment, error) {
	if rp.OrderID != p.ProviderOrderID || rp.Amount != p.AmountCents || !strings.EqualFold(rp.Currency, p.Currency) {
		return nil, errPaymentMismatch
	}
	switch rp.Status {
	case razorpayAuthorized:
		if p.Status != StatusPending {
			// Razorpay refunds payments that are never captured, so there is nothing to undo.
			return p, nil
		}
		if _, err := s.razorpay.capture(ctx, rp.ID, rp.Amount, rp.Currency); err != nil {
			// Auto-capture or another replica may have captured it first.
			current, ferr := s.razorpay.fetchPayment(ctx, rp.ID)
			if ferr != nil || current.Status != razorpayCaptured {
				return nil, err
			}
		}
		return s.captured(ctx, p, rp.ID)
	case razorpayCaptured:
		return s.captured(ctx, p, rp.ID)
	case razorpayFailed:
		if p.Status != StatusPending {
			return p, nil
		}
		reason := rp.ErrorDescription
		if reason == "" {
			reason = "payment failed"
		}
		updated, err := s.fail(ctx, p, reason)
		if errors.Is(err, ErrStatusChanged) {
			return s.repo.GetByID(ctx, p.ID)
		}
		return updated, err
	default:
		return p, nil
	}
}

// captured records that the customer's money was taken for p.
func (s *Service) captured(ctx context.Context, p *Payment, razorpayPaymentID string) (*Payment, error) {
	switch p.Status {
	case StatusPending:
		updated, err := s.repo.Apply(ctx, Transition{
			ID: p.ID, From: StatusPending, To: StatusSucceeded, ProviderRef: razorpayPaymentID,
			Topic: events.TopicPaymentSucceeded, Event: paymentEvent(p, ""),
		})
		if errors.Is(err, ErrStatusChanged) {
			// Cancelled or expired a moment ago; that state is final, so this cannot loop.
			current, gerr := s.repo.GetByID(ctx, p.ID)
			if gerr != nil {
				return nil, gerr
			}
			return s.captured(ctx, current, razorpayPaymentID)
		}
		if err != nil {
			return nil, err
		}
		s.log.Info(ctx, "Payment processed", map[string]interface{}{
			"payment_id": updated.ID, "order_id": updated.OrderID, "status": "SUCCEEDED", "razorpay_payment_id": razorpayPaymentID,
		})
		return updated, nil
	case StatusFailed, StatusCancelled:
		return s.refundLateCapture(ctx, p, razorpayPaymentID)
	default:
		return p, nil
	}
}

// refundLateCapture returns money captured after the order was cancelled or the payment had
// already failed or expired. The order is already cancelled, so no event is published.
func (s *Service) refundLateCapture(ctx context.Context, p *Payment, razorpayPaymentID string) (*Payment, error) {
	if err := s.razorpay.refund(ctx, razorpayPaymentID, p.AmountCents); err != nil {
		return nil, err
	}
	updated, err := s.repo.Apply(ctx, Transition{
		ID: p.ID, From: p.Status, To: StatusRefunded, ProviderRef: razorpayPaymentID,
	})
	if errors.Is(err, ErrStatusChanged) {
		return s.repo.GetByID(ctx, p.ID)
	}
	if err != nil {
		return nil, err
	}
	s.log.Warn(ctx, "Refunded a payment captured after its order was closed", map[string]interface{}{
		"payment_id": p.ID, "order_id": p.OrderID, "razorpay_payment_id": razorpayPaymentID,
	})
	return updated, nil
}

// fail marks a PENDING payment FAILED and publishes payment.failed, which cancels the order.
func (s *Service) fail(ctx context.Context, p *Payment, reason string) (*Payment, error) {
	updated, err := s.repo.Apply(ctx, Transition{
		ID: p.ID, From: StatusPending, To: StatusFailed, FailureReason: reason,
		Topic: events.TopicPaymentFailed, Event: paymentEvent(p, reason),
	})
	if err != nil {
		return nil, err
	}
	s.log.Info(ctx, "Payment failed", map[string]interface{}{
		"payment_id": updated.ID, "order_id": updated.OrderID, "reason": reason,
	})
	return updated, nil
}

const expiredReason = "payment not completed in time"

// ExpirePending fails Razorpay payments still PENDING after the payment timeout, so their orders
// are cancelled and the stock released. Razorpay is asked first, so a payment whose webhook went
// missing is completed instead of failed. Replicas may run this concurrently: each transition is
// guarded by the PENDING status, and refunds are idempotent.
func (s *Service) ExpirePending(ctx context.Context) (int, error) {
	if s.razorpay == nil {
		return 0, nil
	}
	stale, err := s.repo.ListPendingBefore(ctx, ProviderRazorpay, s.now().Add(-s.paymentTimeout), 100)
	if err != nil {
		return 0, err
	}
	expired := 0
	for _, p := range stale {
		done, err := s.expire(ctx, p)
		if err != nil {
			s.log.Warn(ctx, "Could not expire payment, will retry", map[string]interface{}{
				"payment_id": p.ID, "order_id": p.OrderID, "error": err.Error(),
			})
			continue
		}
		if done {
			expired++
		}
	}
	return expired, nil
}

func (s *Service) expire(ctx context.Context, p *Payment) (bool, error) {
	if p.ProviderOrderID != "" {
		attempts, err := s.razorpay.orderPayments(ctx, p.ProviderOrderID)
		if err != nil {
			return false, err
		}
		for i := range attempts {
			if attempts[i].Status == razorpayCaptured || attempts[i].Status == razorpayAuthorized {
				_, err := s.settle(ctx, p, &attempts[i])
				return false, err
			}
		}
	}
	_, err := s.fail(ctx, p, expiredReason)
	if errors.Is(err, ErrStatusChanged) {
		return false, nil
	}
	return err == nil, err
}

// RunExpirySweeper calls ExpirePending every interval until ctx is cancelled.
func (s *Service) RunExpirySweeper(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n, err := s.ExpirePending(ctx)
			if err != nil {
				s.log.Error(ctx, "Payment expiry sweep failed", map[string]interface{}{"error": err.Error()})
			} else if n > 0 {
				s.log.Info(ctx, "Expired unpaid payments", map[string]interface{}{"count": n})
			}
		}
	}
}

// isProviderUnavailable reports whether err came from Razorpay being unreachable or failing.
func isProviderUnavailable(err error) bool {
	var apiErr *RazorpayError
	var netErr net.Error
	return errors.As(err, &apiErr) || errors.As(err, &netErr) || errors.Is(err, context.DeadlineExceeded)
}

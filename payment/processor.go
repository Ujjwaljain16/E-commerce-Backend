package payment

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// ChargeRequest is what a payment provider needs to charge an order.
type ChargeRequest struct {
	PaymentID      string
	OrderID        string
	AmountCents    int64
	Currency       string
	Method         string
	IdempotencyKey string
}

// RefundRequest reverses a previous successful charge.
type RefundRequest struct {
	ProviderRef string
	AmountCents int64
	Currency    string
	Reason      string
}

// DeclineError means the provider rejected the charge. Unlike other errors it is a final answer,
// so the payment is marked FAILED rather than retried.
type DeclineError struct {
	Reason string
}

func (e *DeclineError) Error() string { return "payment declined: " + e.Reason }

// Processor talks to a payment provider.
type Processor interface {
	// Charge returns the provider's reference for the charge, or a *DeclineError.
	Charge(ctx context.Context, req ChargeRequest) (string, error)
	Refund(ctx context.Context, req RefundRequest) error
}

// mockChargeLimitCents is the largest amount MockProcessor will accept ($10,000), so large orders can
// exercise the failure path.
const mockChargeLimitCents = 1_000_000

// MockProcessor approves everything except the "card_declined" method and amounts above $10,000.
type MockProcessor struct{}

// Charge implements Processor.
func (MockProcessor) Charge(_ context.Context, req ChargeRequest) (string, error) {
	if req.Method == "card_declined" {
		return "", &DeclineError{Reason: "card declined"}
	}
	if req.AmountCents > mockChargeLimitCents {
		return "", &DeclineError{Reason: "amount exceeds limit"}
	}
	return "mock_" + uuid.NewString(), nil
}

// Refund implements Processor.
func (MockProcessor) Refund(_ context.Context, req RefundRequest) error {
	if req.ProviderRef == "" {
		return fmt.Errorf("refund needs a provider reference")
	}
	return nil
}

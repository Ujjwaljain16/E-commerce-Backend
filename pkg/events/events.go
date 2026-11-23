// Package events defines the Kafka topics and JSON payloads exchanged between services.
// Every event is keyed by order ID so all events for one order land on the same partition.
package events

import (
	"time"

	"github.com/google/uuid"
)

// Kafka topics.
const (
	TopicOrderCreated     = "order.created"
	TopicOrderConfirmed   = "order.confirmed"
	TopicOrderCancelled   = "order.cancelled"
	TopicPaymentSucceeded = "payment.succeeded"
	TopicPaymentFailed    = "payment.failed"
	TopicPaymentRefunded  = "payment.refunded"
)

// Meta is embedded in every event. EventID lets consumers deduplicate redeliveries.
type Meta struct {
	EventID    string    `json:"event_id"`
	OccurredAt time.Time `json:"occurred_at"`
}

// NewMeta returns a Meta with a fresh ID and the current time.
func NewMeta() Meta {
	return Meta{EventID: uuid.NewString(), OccurredAt: time.Now().UTC()}
}

// OrderItem is a line item as priced by the catalog at reservation time.
// Money is always an integer number of minor units (cents for USD), never a float.
type OrderItem struct {
	ProductID   string `json:"product_id"`
	ProductName string `json:"product_name"`
	Quantity    int32  `json:"quantity"`
	PriceCents  int64  `json:"price_cents"`
}

// OrderCreated is published once an order and its stock reservation are committed.
type OrderCreated struct {
	Meta
	OrderID          string      `json:"order_id"`
	UserID           string      `json:"user_id"`
	TotalAmountCents int64       `json:"total_amount_cents"`
	Currency         string      `json:"currency"`
	PaymentMethod    string      `json:"payment_method"`
	Items            []OrderItem `json:"items"`
}

// OrderConfirmed is published when payment succeeded and the order moved to CONFIRMED.
type OrderConfirmed struct {
	Meta
	OrderID          string `json:"order_id"`
	UserID           string `json:"user_id"`
	TotalAmountCents int64  `json:"total_amount_cents"`
}

// OrderCancelled is published when an order is cancelled by the user or after a failed payment.
type OrderCancelled struct {
	Meta
	OrderID string `json:"order_id"`
	UserID  string `json:"user_id"`
	Reason  string `json:"reason"`
}

// Payment is the payload of payment.succeeded, payment.failed and payment.refunded.
type Payment struct {
	Meta
	PaymentID   string `json:"payment_id"`
	OrderID     string `json:"order_id"`
	UserID      string `json:"user_id"`
	AmountCents int64  `json:"amount_cents"`
	Currency    string `json:"currency"`
	Reason      string `json:"reason,omitempty"`
}

package order

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/events"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/kafka"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	kafkago "github.com/segmentio/kafka-go"
)

// PaymentTopics are the topics the order service consumes.
var PaymentTopics = []string{ //nolint:gochecknoglobals // constant list of topic names
	events.TopicPaymentSucceeded, events.TopicPaymentFailed, events.TopicPaymentRefunded,
}

// PaymentEventHandler applies payment outcomes to orders.
type PaymentEventHandler struct {
	repo Repository
	log  *logger.Logger
}

// NewPaymentEventHandler creates the handler for payment events.
func NewPaymentEventHandler(repo Repository, log *logger.Logger) *PaymentEventHandler {
	return &PaymentEventHandler{repo: repo, log: log}
}

// Handle processes one payment event. All paths are idempotent so redeliveries are harmless.
func (h *PaymentEventHandler) Handle(ctx context.Context, msg kafkago.Message) error {
	var evt events.Payment
	if err := json.Unmarshal(msg.Value, &evt); err != nil || evt.OrderID == "" {
		return fmt.Errorf("%w: undecodable %s event", kafka.ErrSkip, msg.Topic)
	}

	switch msg.Topic {
	case events.TopicPaymentSucceeded, events.TopicPaymentFailed:
		succeeded := msg.Topic == events.TopicPaymentSucceeded
		applied, err := h.repo.ApplyPaymentResult(ctx, evt.OrderID, succeeded, evt.Reason)
		if err != nil {
			return err
		}
		h.log.Info(ctx, "payment result applied", map[string]interface{}{
			"order_id": evt.OrderID, "succeeded": succeeded, "changed": applied,
		})
	case events.TopicPaymentRefunded:
		if err := h.repo.MarkRefunded(ctx, evt.OrderID); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: unexpected topic %s", kafka.ErrSkip, msg.Topic)
	}
	return nil
}

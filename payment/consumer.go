package payment

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/events"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/kafka"
	kafkago "github.com/segmentio/kafka-go"
)

// ConsumedTopics are the order events the payment service reacts to.
var ConsumedTopics = []string{events.TopicOrderCreated, events.TopicOrderCancelled}

// HandleMessage is the kafka.Handler for order events. Messages that can never succeed are
// wrapped in kafka.ErrSkip; anything else is returned so the consumer retries it.
func (s *Service) HandleMessage(ctx context.Context, msg kafkago.Message) error {
	var err error
	switch msg.Topic {
	case events.TopicOrderCreated:
		var e events.OrderCreated
		if jerr := json.Unmarshal(msg.Value, &e); jerr != nil {
			return fmt.Errorf("%w: decode %s: %v", kafka.ErrSkip, msg.Topic, jerr)
		}
		_, err = s.Process(ctx, ProcessInput{
			OrderID:        e.OrderID,
			UserID:         e.UserID,
			AmountCents:    e.TotalAmountCents,
			Currency:       e.Currency,
			Method:         e.PaymentMethod,
			IdempotencyKey: e.OrderID,
		})
	case events.TopicOrderCancelled:
		var e events.OrderCancelled
		if jerr := json.Unmarshal(msg.Value, &e); jerr != nil {
			return fmt.Errorf("%w: decode %s: %v", kafka.ErrSkip, msg.Topic, jerr)
		}
		err = s.CancelOrder(ctx, e.OrderID, e.UserID, e.Reason)
	default:
		return fmt.Errorf("%w: unexpected topic %s", kafka.ErrSkip, msg.Topic)
	}

	if IsInvalidInput(err) {
		return fmt.Errorf("%w: %s: %v", kafka.ErrSkip, msg.Topic, err)
	}
	return err
}

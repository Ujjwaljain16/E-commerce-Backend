package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/events"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/kafka"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	kafkago "github.com/segmentio/kafka-go"
)

// OrderTopics are the order events the catalog consumes.
var OrderTopics = []string{events.TopicOrderCreated, events.TopicOrderCancelled} //nolint:gochecknoglobals // constant topic list

// OrderEventHandler keeps stock reservations in step with the order lifecycle: order.created
// commits the reservation, order.cancelled (customer, admin or failed payment) returns the stock.
type OrderEventHandler struct {
	repo Repository
	log  *logger.Logger
}

// NewOrderEventHandler creates the handler.
func NewOrderEventHandler(repo Repository, log *logger.Logger) *OrderEventHandler {
	return &OrderEventHandler{repo: repo, log: log}
}

// Handle processes one order event. Both paths are idempotent, so redelivery is safe.
func (h *OrderEventHandler) Handle(ctx context.Context, msg kafkago.Message) error {
	var evt struct {
		OrderID string `json:"order_id"`
	}
	if err := json.Unmarshal(msg.Value, &evt); err != nil || evt.OrderID == "" {
		return fmt.Errorf("%w: undecodable %s event", kafka.ErrSkip, msg.Topic)
	}

	var err error
	switch msg.Topic {
	case events.TopicOrderCreated:
		_, err = h.repo.CommitReservation(ctx, evt.OrderID)
		if errors.Is(err, ErrReservationLost) {
			// The reservation expired before the order event arrived and the stock is gone.
			// Retrying cannot fix this; it needs a human (or a refund), so make it loud.
			h.log.Error(ctx, "order placed on an expired reservation that could not be restored", map[string]interface{}{
				"order_id": evt.OrderID, "error": err.Error(),
			})
			return fmt.Errorf("%w: %v", kafka.ErrSkip, err)
		}
	case events.TopicOrderCancelled:
		var ids []string
		ids, err = h.repo.ReleaseStock(ctx, evt.OrderID)
		if err == nil && len(ids) > 0 {
			h.log.Info(ctx, "Stock released for cancelled order", map[string]interface{}{"order_id": evt.OrderID, "products": len(ids)})
		}
	default:
		return fmt.Errorf("%w: unexpected topic %s", kafka.ErrSkip, msg.Topic)
	}
	if err != nil && isInvalidID(err) {
		return fmt.Errorf("%w: invalid order id %q", kafka.ErrSkip, evt.OrderID)
	}
	return err
}

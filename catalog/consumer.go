package catalog

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/events"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/kafka"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	kafkago "github.com/segmentio/kafka-go"
)

// OrderCancelledHandler releases an order's reserved stock when the order is cancelled
// (by the customer, an admin, or after a failed payment).
type OrderCancelledHandler struct {
	repo Repository
	log  *logger.Logger
}

// NewOrderCancelledHandler creates the handler.
func NewOrderCancelledHandler(repo Repository, log *logger.Logger) *OrderCancelledHandler {
	return &OrderCancelledHandler{repo: repo, log: log}
}

// Handle releases stock; ReleaseStock is idempotent so redelivery is safe.
func (h *OrderCancelledHandler) Handle(ctx context.Context, msg kafkago.Message) error {
	var evt events.OrderCancelled
	if err := json.Unmarshal(msg.Value, &evt); err != nil || evt.OrderID == "" {
		return fmt.Errorf("%w: undecodable order.cancelled event", kafka.ErrSkip)
	}
	ids, err := h.repo.ReleaseStock(ctx, evt.OrderID)
	if err != nil {
		if isInvalidID(err) {
			return fmt.Errorf("%w: invalid order id %q", kafka.ErrSkip, evt.OrderID)
		}
		return err
	}
	if len(ids) > 0 {
		h.log.Info(ctx, "Stock released for cancelled order", map[string]interface{}{"order_id": evt.OrderID, "products": len(ids)})
	}
	return nil
}

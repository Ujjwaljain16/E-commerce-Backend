package order

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/events"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/outbox"
	"github.com/google/uuid"
)

// Cancel cancels a PENDING or CONFIRMED order. The status change and the order.cancelled event
// commit together; catalog releases the stock and payment refunds when they consume the event.
func (r *repository) Cancel(ctx context.Context, id, reason string) (*Order, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrOrderNotFound
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin cancel: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	var userID string
	err = tx.QueryRowContext(ctx, `
		UPDATE orders SET status = $1, updated_at = NOW()
		WHERE id = $2 AND status IN ($3, $4)
		RETURNING user_id`,
		OrderStatusCancelled, id, OrderStatusPending, OrderStatusConfirmed).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		if _, gerr := r.GetByID(ctx, id); gerr != nil {
			return nil, gerr
		}
		return nil, ErrInvalidStatus
	}
	if err != nil {
		return nil, fmt.Errorf("cancel order: %w", err)
	}

	evt := events.OrderCancelled{Meta: events.NewMeta(), OrderID: id, UserID: userID, Reason: reason}
	if err := outbox.Add(ctx, tx, OutboxTable, events.TopicOrderCancelled, id, evt); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit cancel: %w", err)
	}
	return r.GetByID(ctx, id)
}

// ApplyPaymentResult confirms (on success) or cancels (on failure) an order that is still PENDING.
// Redelivered or late payment events find the order already moved on and change nothing.
func (r *repository) ApplyPaymentResult(ctx context.Context, id string, succeeded bool, reason string) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin payment result: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	next := OrderStatusConfirmed
	if !succeeded {
		next = OrderStatusCancelled
	}
	var userID, currency string
	var total int64
	err = tx.QueryRowContext(ctx, `
		UPDATE orders SET status = $1, updated_at = NOW()
		WHERE id = $2 AND status = $3
		RETURNING user_id, total_amount_cents, currency`,
		next, id, OrderStatusPending).Scan(&userID, &total, &currency)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("apply payment result: %w", err)
	}

	if succeeded {
		err = outbox.Add(ctx, tx, OutboxTable, events.TopicOrderConfirmed, id,
			events.OrderConfirmed{Meta: events.NewMeta(), OrderID: id, UserID: userID, TotalAmountCents: total, Currency: currency})
	} else {
		err = outbox.Add(ctx, tx, OutboxTable, events.TopicOrderCancelled, id,
			events.OrderCancelled{Meta: events.NewMeta(), OrderID: id, UserID: userID, Reason: "payment failed: " + reason})
	}
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit payment result: %w", err)
	}
	return true, nil
}

// MarkRefunded flags an order as REFUNDED unless it was cancelled (a cancelled order keeps that status).
func (r *repository) MarkRefunded(ctx context.Context, id string) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2 AND status NOT IN ($3, $1)`,
		OrderStatusRefunded, id, OrderStatusCancelled)
	if err != nil {
		return fmt.Errorf("mark refunded: %w", err)
	}
	return nil
}

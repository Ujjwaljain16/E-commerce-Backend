package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	"github.com/lib/pq"
)

// Reservation lifecycle:
//
//	RESERVED ──order.created──▶ COMMITTED ──order.cancelled──▶ RELEASED
//	   │                                                          ▲
//	   ├──order.cancelled─────────────────────────────────────────┘
//	   └──no order.created within the TTL──▶ EXPIRED ──late order.created──▶ COMMITTED (if stock allows)
//
// An order is only persisted if its reservation succeeded, and its order.created event commits
// atomically with it, so a reservation that never sees order.created belongs to an order that
// failed to save. The sweeper gives that stock back.

// ErrReservationLost is returned when a late order.created finds its expired reservation can no
// longer be honoured because the stock has been sold since.
var ErrReservationLost = errors.New("expired reservation could not be restored")

// CommitReservation records that the order behind a reservation exists.
func (r *postgresRepository) CommitReservation(ctx context.Context, orderID string) ([]string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin commit: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM stock_reservations WHERE order_id = $1 FOR UPDATE`, orderID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if err != nil {
		return nil, fmt.Errorf("load reservation: %w", err)
	}

	var touched []string
	switch status {
	case "COMMITTED", "RELEASED":
		// Already committed, or cancelled before this event arrived: nothing to do.
		return nil, tx.Commit()
	case "EXPIRED":
		items, err := reservationItems(ctx, tx, orderID)
		if err != nil {
			return nil, err
		}
		for _, it := range items {
			res, err := tx.ExecContext(ctx, `UPDATE products SET stock = stock - $2 WHERE id = $1 AND stock >= $2`, it.ProductID, it.Quantity)
			if err != nil {
				return nil, fmt.Errorf("re-reserve %s: %w", it.ProductID, err)
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return nil, &StockError{ProductID: it.ProductID, Err: ErrReservationLost}
			}
			touched = append(touched, it.ProductID)
		}
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE stock_reservations SET status = 'COMMITTED', updated_at = NOW() WHERE order_id = $1`, orderID); err != nil {
		return nil, fmt.Errorf("mark committed: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit reservation: %w", err)
	}
	return touched, nil
}

// ExpiredReservations lists reservations still RESERVED after ttl.
func (r *postgresRepository) ExpiredReservations(ctx context.Context, ttl time.Duration, limit int) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT order_id FROM stock_reservations
		WHERE status = 'RESERVED' AND created_at < NOW() - make_interval(secs => $1)
		ORDER BY created_at
		LIMIT $2`, ttl.Seconds(), limit)
	if err != nil {
		return nil, fmt.Errorf("list expired reservations: %w", err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ExpireReservation returns the stock of a reservation that is still RESERVED.
func (r *postgresRepository) ExpireReservation(ctx context.Context, orderID string) ([]string, error) {
	return r.returnStock(ctx, orderID, "EXPIRED", "RESERVED")
}

// returnStock moves a reservation from one of the given statuses to `to` and puts its stock back.
// The status guard makes concurrent releases (several replicas, redelivered events) harmless.
func (r *postgresRepository) returnStock(ctx context.Context, orderID, to string, from ...string) ([]string, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin release: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	res, err := tx.ExecContext(ctx,
		`UPDATE stock_reservations SET status = $2, updated_at = NOW() WHERE order_id = $1 AND status = ANY($3)`,
		orderID, to, pq.Array(from))
	if err != nil {
		return nil, fmt.Errorf("mark %s: %w", to, err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, tx.Commit()
	}

	items, err := reservationItems(ctx, tx, orderID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(items))
	for _, it := range items {
		if _, err := tx.ExecContext(ctx, `UPDATE products SET stock = stock + $2 WHERE id = $1`, it.ProductID, it.Quantity); err != nil {
			return nil, fmt.Errorf("restock %s: %w", it.ProductID, err)
		}
		ids = append(ids, it.ProductID)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit release: %w", err)
	}
	return ids, nil
}

// ReservationSweeper periodically expires reservations whose order never got created.
type ReservationSweeper struct {
	repo     Repository
	log      *logger.Logger
	ttl      time.Duration
	interval time.Duration
}

// NewReservationSweeper creates a sweeper that expires reservations older than ttl.
func NewReservationSweeper(repo Repository, log *logger.Logger, ttl time.Duration) *ReservationSweeper {
	return &ReservationSweeper{repo: repo, log: log, ttl: ttl, interval: time.Minute}
}

// Run sweeps until ctx is cancelled.
func (s *ReservationSweeper) Run(ctx context.Context) {
	t := time.NewTicker(s.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.SweepOnce(ctx)
		}
	}
}

// SweepOnce expires one batch of stale reservations and returns how many it released.
func (s *ReservationSweeper) SweepOnce(ctx context.Context) int {
	ids, err := s.repo.ExpiredReservations(ctx, s.ttl, 100)
	if err != nil {
		if ctx.Err() == nil {
			s.log.Error(ctx, "reservation sweep failed", map[string]interface{}{"error": err.Error()})
		}
		return 0
	}
	released := 0
	for _, id := range ids {
		products, err := s.repo.ExpireReservation(ctx, id)
		if err != nil {
			s.log.Error(ctx, "failed to expire reservation", map[string]interface{}{"order_id": id, "error": err.Error()})
			continue
		}
		if len(products) > 0 {
			released++
			s.log.Warn(ctx, "expired orphaned stock reservation", map[string]interface{}{"order_id": id, "products": len(products)})
		}
	}
	return released
}

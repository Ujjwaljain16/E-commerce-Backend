package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"

	"github.com/Ujjwaljain16/E-commerce-Backend/catalog/pb"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	// ErrProductNotFound is returned when a product does not exist
	ErrProductNotFound = errors.New("product not found")
	// ErrInsufficientStock is returned when a reservation asks for more than is in stock
	ErrInsufficientStock = errors.New("insufficient stock")
)

// StockItem is a requested quantity of one product.
type StockItem struct {
	ProductID string
	Quantity  int32
}

// ReservedItem is a reserved line item with the product's name and price at reservation time.
type ReservedItem struct {
	ProductID      string
	ProductName    string
	Quantity       int32
	UnitPriceCents int64
}

// StockError carries the product that caused a reservation to fail.
type StockError struct {
	ProductID string
	Err       error
}

func (e *StockError) Error() string { return fmt.Sprintf("%s: %v", e.ProductID, e.Err) }
func (e *StockError) Unwrap() error { return e.Err }

// ReserveStock decrements stock for all items in one transaction. Calling it again for the same
// order returns the original reservation instead of reserving twice.
func (r *postgresRepository) ReserveStock(ctx context.Context, orderID string, items []StockItem) ([]ReservedItem, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin reservation: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after commit

	res, err := tx.ExecContext(ctx,
		`INSERT INTO stock_reservations (order_id, status) VALUES ($1, 'RESERVED') ON CONFLICT (order_id) DO NOTHING`, orderID)
	if err != nil {
		return nil, fmt.Errorf("insert reservation: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		existing, err := reservationItems(ctx, tx, orderID)
		if err != nil {
			return nil, err
		}
		return existing, tx.Commit()
	}

	// Locking rows in a fixed order prevents deadlocks between concurrent orders for the same products.
	sorted := append([]StockItem(nil), items...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ProductID < sorted[j].ProductID })

	reserved := make([]ReservedItem, 0, len(sorted))
	for _, it := range sorted {
		item := ReservedItem{ProductID: it.ProductID, Quantity: it.Quantity}
		err := tx.QueryRowContext(ctx,
			`UPDATE products SET stock = stock - $2 WHERE id = $1 AND stock >= $2 RETURNING name, price_cents`,
			it.ProductID, it.Quantity).Scan(&item.ProductName, &item.UnitPriceCents)
		if errors.Is(err, sql.ErrNoRows) {
			var exists bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM products WHERE id = $1)`, it.ProductID).Scan(&exists); err != nil {
				return nil, fmt.Errorf("check product: %w", err)
			}
			if !exists {
				return nil, &StockError{ProductID: it.ProductID, Err: ErrProductNotFound}
			}
			return nil, &StockError{ProductID: it.ProductID, Err: ErrInsufficientStock}
		}
		if err != nil {
			return nil, fmt.Errorf("reserve %s: %w", it.ProductID, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO stock_reservation_items (order_id, product_id, product_name, quantity, unit_price_cents) VALUES ($1, $2, $3, $4, $5)`,
			orderID, item.ProductID, item.ProductName, item.Quantity, item.UnitPriceCents); err != nil {
			return nil, fmt.Errorf("insert reservation item: %w", err)
		}
		reserved = append(reserved, item)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit reservation: %w", err)
	}
	return reserved, nil
}

// ReleaseStock puts reserved stock back. It reports false when nothing was reserved, which makes
// repeated releases (event redelivery, retries) harmless.
func (r *postgresRepository) ReleaseStock(ctx context.Context, orderID string) ([]string, error) {
	return r.returnStock(ctx, orderID, "RELEASED", "RESERVED", "COMMITTED")
}

func reservationItems(ctx context.Context, tx *sql.Tx, orderID string) ([]ReservedItem, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT product_id, product_name, quantity, unit_price_cents FROM stock_reservation_items WHERE order_id = $1 ORDER BY product_id`, orderID)
	if err != nil {
		return nil, fmt.Errorf("load reservation items: %w", err)
	}
	defer rows.Close()
	var items []ReservedItem
	for rows.Next() {
		var it ReservedItem
		if err := rows.Scan(&it.ProductID, &it.ProductName, &it.Quantity, &it.UnitPriceCents); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// ReserveStock validates and reserves stock for an order and returns authoritative prices.
func (s *Service) ReserveStock(ctx context.Context, req *pb.ReserveStockRequest) (*pb.ReserveStockResponse, error) {
	if _, err := uuid.Parse(req.OrderId); err != nil {
		return nil, status.Error(codes.InvalidArgument, "order_id must be a UUID")
	}
	if len(req.Items) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one item is required")
	}

	// Duplicate lines for the same product are merged so the stock check sees the full quantity.
	merged := map[string]int32{}
	var order []string
	for _, it := range req.Items {
		if _, err := uuid.Parse(it.ProductId); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "product_id %q is not a valid id", it.ProductId)
		}
		if it.Quantity <= 0 {
			return nil, status.Error(codes.InvalidArgument, "quantity must be positive")
		}
		if _, seen := merged[it.ProductId]; !seen {
			order = append(order, it.ProductId)
		}
		merged[it.ProductId] += it.Quantity
	}
	items := make([]StockItem, 0, len(order))
	for _, id := range order {
		items = append(items, StockItem{ProductID: id, Quantity: merged[id]})
	}

	reserved, err := s.repo.ReserveStock(ctx, req.OrderId, items)
	if err != nil {
		var se *StockError
		switch {
		case errors.As(err, &se) && errors.Is(err, ErrProductNotFound):
			return nil, status.Errorf(codes.NotFound, "product %s not found", se.ProductID)
		case errors.As(err, &se) && errors.Is(err, ErrInsufficientStock):
			return nil, status.Errorf(codes.FailedPrecondition, "insufficient stock for product %s", se.ProductID)
		}
		s.log.Error(ctx, "Failed to reserve stock", map[string]interface{}{"error": err.Error(), "order_id": req.OrderId})
		return nil, status.Error(codes.Internal, "failed to reserve stock")
	}

	resp := &pb.ReserveStockResponse{Items: make([]*pb.ReservedItem, len(reserved))}
	for i, it := range reserved {
		resp.Items[i] = &pb.ReservedItem{
			ProductId: it.ProductID, ProductName: it.ProductName, Quantity: it.Quantity, UnitPriceCents: it.UnitPriceCents,
		}
	}
	s.log.Info(ctx, "Stock reserved", map[string]interface{}{"order_id": req.OrderId, "items": len(reserved)})
	return resp, nil
}

// ReleaseStock returns an order's reserved stock. Safe to call more than once.
func (s *Service) ReleaseStock(ctx context.Context, req *pb.ReleaseStockRequest) (*pb.ReleaseStockResponse, error) {
	if _, err := uuid.Parse(req.OrderId); err != nil {
		return nil, status.Error(codes.InvalidArgument, "order_id must be a UUID")
	}
	ids, err := s.repo.ReleaseStock(ctx, req.OrderId)
	if err != nil {
		s.log.Error(ctx, "Failed to release stock", map[string]interface{}{"error": err.Error(), "order_id": req.OrderId})
		return nil, status.Error(codes.Internal, "failed to release stock")
	}
	if len(ids) > 0 {
		s.log.Info(ctx, "Stock released", map[string]interface{}{"order_id": req.OrderId, "items": len(ids)})
	}
	return &pb.ReleaseStockResponse{Released: len(ids) > 0}, nil
}

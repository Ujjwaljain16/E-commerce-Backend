package order

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/events"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/outbox"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// OutboxTable holds order events until the relay publishes them to Kafka.
const OutboxTable = "order_outbox"

// DefaultCurrency is used when no CURRENCY is configured.
const DefaultCurrency = "USD"

var (
	// ErrOrderNotFound is returned when an order is not found
	ErrOrderNotFound = errors.New("order not found")
	// ErrInvalidStatus is returned when an invalid status transition is attempted
	ErrInvalidStatus = errors.New("invalid order status")
	// ErrEmptyOrderItems is returned when attempting to create an order without items
	ErrEmptyOrderItems = errors.New("order must have at least one item")
	// ErrTotalTooLarge is returned when a subtotal or the order total does not fit in int64 cents
	ErrTotalTooLarge = errors.New("order total is too large")
)

// OrderStatus represents the state of an order
type OrderStatus int

const (
	OrderStatusPending    OrderStatus = 1
	OrderStatusConfirmed  OrderStatus = 2
	OrderStatusProcessing OrderStatus = 3
	OrderStatusShipped    OrderStatus = 4
	OrderStatusDelivered  OrderStatus = 5
	OrderStatusCancelled  OrderStatus = 6
	OrderStatusRefunded   OrderStatus = 7
)

// OrderItem represents an item in an order
type OrderItem struct {
	ID            string
	OrderID       string
	ProductID     string
	ProductName   string
	Quantity      int
	PriceCents    int64
	SubtotalCents int64
	CreatedAt     time.Time
}

// Order represents a customer order
type Order struct {
	ID               string
	UserID           string
	Status           OrderStatus
	Items            []OrderItem
	TotalAmountCents int64
	ShippingAddress  string
	BillingAddress   string
	PaymentMethod    string
	// Currency is the ISO 4217 code the amounts are in; empty means DefaultCurrency.
	Currency  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Repository defines the interface for order data operations
type Repository interface {
	Create(ctx context.Context, order *Order) (*Order, error)
	GetByID(ctx context.Context, id string) (*Order, error)
	List(ctx context.Context, page, pageSize int, userID string, status OrderStatus) ([]*Order, int, error)
	UpdateStatus(ctx context.Context, id string, status OrderStatus) (*Order, error)
	// Cancel moves a PENDING or CONFIRMED order to CANCELLED and records an order.cancelled event.
	Cancel(ctx context.Context, id, reason string) (*Order, error)
	// ApplyPaymentResult confirms or cancels a PENDING order; applied is false if the order had already moved on.
	ApplyPaymentResult(ctx context.Context, id string, succeeded bool, reason string) (applied bool, err error)
	// MarkRefunded records that the order's payment was refunded.
	MarkRefunded(ctx context.Context, id string) error
	GetByUserID(ctx context.Context, userID string, page, pageSize int) ([]*Order, int, error)
	Close() error
}

type repository struct {
	db *sql.DB
}

// NewRepository creates a new order repository
func NewRepository(db *sql.DB) Repository {
	return &repository{db: db}
}

// Create creates a new order with items in a transaction
func (r *repository) Create(ctx context.Context, order *Order) (*Order, error) {
	if len(order.Items) == 0 {
		return nil, ErrEmptyOrderItems
	}
	if err := priceOrder(order); err != nil {
		return nil, err
	}

	// Start transaction
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	if order.ID == "" {
		order.ID = uuid.New().String()
	}
	order.CreatedAt = time.Now()
	order.UpdatedAt = time.Now()
	order.Status = OrderStatusPending
	if order.Currency == "" {
		order.Currency = DefaultCurrency
	}

	// Insert order
	orderQuery := `
		INSERT INTO orders (id, user_id, status, total_amount_cents, shipping_address, billing_address, payment_method, created_at, updated_at, currency)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err = tx.ExecContext(ctx, orderQuery,
		order.ID,
		order.UserID,
		order.Status,
		order.TotalAmountCents,
		order.ShippingAddress,
		order.BillingAddress,
		order.PaymentMethod,
		order.CreatedAt,
		order.UpdatedAt,
		order.Currency,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert order: %w", err)
	}

	// Insert order items
	itemQuery := `
		INSERT INTO order_items (id, order_id, product_id, product_name, quantity, price_cents, subtotal_cents, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	for i := range order.Items {
		order.Items[i].ID = uuid.New().String()
		order.Items[i].OrderID = order.ID
		order.Items[i].CreatedAt = time.Now()

		_, err = tx.ExecContext(ctx, itemQuery,
			order.Items[i].ID,
			order.Items[i].OrderID,
			order.Items[i].ProductID,
			order.Items[i].ProductName,
			order.Items[i].Quantity,
			order.Items[i].PriceCents,
			order.Items[i].SubtotalCents,
			order.Items[i].CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to insert order item: %w", err)
		}
	}

	created := events.OrderCreated{
		Meta:             events.NewMeta(),
		OrderID:          order.ID,
		UserID:           order.UserID,
		TotalAmountCents: order.TotalAmountCents,
		Currency:         order.Currency,
		PaymentMethod:    order.PaymentMethod,
	}
	for _, it := range order.Items {
		created.Items = append(created.Items, events.OrderItem{
			ProductID: it.ProductID, ProductName: it.ProductName, Quantity: int32(it.Quantity), PriceCents: it.PriceCents,
		})
	}
	if err = outbox.Add(ctx, tx, OutboxTable, events.TopicOrderCreated, order.ID, created); err != nil {
		return nil, err
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return order, nil
}

// GetByID retrieves an order by ID with its items
func (r *repository) GetByID(ctx context.Context, id string) (*Order, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrOrderNotFound
	}
	// Get order
	orderQuery := `
		SELECT id, user_id, status, total_amount_cents, shipping_address, billing_address, payment_method, created_at, updated_at
		FROM orders
		WHERE id = $1
	`
	order := &Order{}
	err := r.db.QueryRowContext(ctx, orderQuery, id).Scan(
		&order.ID,
		&order.UserID,
		&order.Status,
		&order.TotalAmountCents,
		&order.ShippingAddress,
		&order.BillingAddress,
		&order.PaymentMethod,
		&order.CreatedAt,
		&order.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, ErrOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get order: %w", err)
	}

	// Get order items
	itemsQuery := `
		SELECT id, order_id, product_id, product_name, quantity, price_cents, subtotal_cents, created_at
		FROM order_items
		WHERE order_id = $1
		ORDER BY created_at
	`
	rows, err := r.db.QueryContext(ctx, itemsQuery, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get order items: %w", err)
	}
	defer rows.Close()

	order.Items = []OrderItem{}
	for rows.Next() {
		item := OrderItem{}
		err = rows.Scan(
			&item.ID,
			&item.OrderID,
			&item.ProductID,
			&item.ProductName,
			&item.Quantity,
			&item.PriceCents,
			&item.SubtotalCents,
			&item.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan order item: %w", err)
		}
		order.Items = append(order.Items, item)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating order items: %w", err)
	}

	return order, nil
}

// List retrieves orders with pagination and optional filters
func (r *repository) List(ctx context.Context, page, pageSize int, userID string, status OrderStatus) ([]*Order, int, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	// Build query with optional filters
	query := `
		SELECT id, user_id, status, total_amount_cents, shipping_address, billing_address, payment_method, created_at, updated_at
		FROM orders
		WHERE 1=1
	`
	countQuery := "SELECT COUNT(*) FROM orders WHERE 1=1"
	args := []interface{}{}
	argCount := 1

	if userID != "" {
		query += fmt.Sprintf(" AND user_id = $%d", argCount)
		countQuery += fmt.Sprintf(" AND user_id = $%d", argCount)
		args = append(args, userID)
		argCount++
	}

	if status > 0 {
		query += fmt.Sprintf(" AND status = $%d", argCount)
		countQuery += fmt.Sprintf(" AND status = $%d", argCount)
		args = append(args, status)
		argCount++
	}

	// Get total count
	var total int
	err := r.db.QueryRowContext(ctx, countQuery, args...).Scan(&total)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to count orders: %w", err)
	}

	// Add pagination
	query += fmt.Sprintf(" ORDER BY created_at DESC LIMIT $%d OFFSET $%d", argCount, argCount+1)
	args = append(args, pageSize, offset)

	// Get orders
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list orders: %w", err)
	}
	defer rows.Close()

	orders := []*Order{}
	for rows.Next() {
		order := &Order{}
		err = rows.Scan(
			&order.ID,
			&order.UserID,
			&order.Status,
			&order.TotalAmountCents,
			&order.ShippingAddress,
			&order.BillingAddress,
			&order.PaymentMethod,
			&order.CreatedAt,
			&order.UpdatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to scan order: %w", err)
		}

		// Get items for this order
		order.Items, err = r.getOrderItems(ctx, order.ID)
		if err != nil {
			return nil, 0, err
		}

		orders = append(orders, order)
	}

	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("error iterating orders: %w", err)
	}

	return orders, total, nil
}

// getOrderItems is a helper function to retrieve items for an order
func (r *repository) getOrderItems(ctx context.Context, orderID string) ([]OrderItem, error) {
	query := `
		SELECT id, order_id, product_id, product_name, quantity, price_cents, subtotal_cents, created_at
		FROM order_items
		WHERE order_id = $1
		ORDER BY created_at
	`
	rows, err := r.db.QueryContext(ctx, query, orderID)
	if err != nil {
		return nil, fmt.Errorf("failed to get order items: %w", err)
	}
	defer rows.Close()

	items := []OrderItem{}
	for rows.Next() {
		item := OrderItem{}
		err = rows.Scan(
			&item.ID,
			&item.OrderID,
			&item.ProductID,
			&item.ProductName,
			&item.Quantity,
			&item.PriceCents,
			&item.SubtotalCents,
			&item.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan order item: %w", err)
		}
		items = append(items, item)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating order items: %w", err)
	}

	return items, nil
}

// allowedFrom lists the statuses an order may move to each status from via UpdateStatus.
// Cancellation and refunds have their own paths because they emit events.
var allowedFrom = map[OrderStatus][]int64{ //nolint:gochecknoglobals // read-only lookup table
	OrderStatusConfirmed:  {int64(OrderStatusPending)},
	OrderStatusProcessing: {int64(OrderStatusConfirmed)},
	OrderStatusShipped:    {int64(OrderStatusProcessing)},
	OrderStatusDelivered:  {int64(OrderStatusShipped)},
}

// UpdateStatus moves an order forward along the fulfilment flow.
func (r *repository) UpdateStatus(ctx context.Context, id string, status OrderStatus) (*Order, error) {
	if _, err := uuid.Parse(id); err != nil {
		return nil, ErrOrderNotFound
	}
	from, ok := allowedFrom[status]
	if !ok {
		return nil, ErrInvalidStatus
	}
	query := `
		UPDATE orders
		SET status = $1, updated_at = $2
		WHERE id = $3 AND status = ANY($4)
	`
	result, err := r.db.ExecContext(ctx, query, status, time.Now(), id, pq.Array(from))
	if err != nil {
		return nil, fmt.Errorf("failed to update order status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		if _, err := r.GetByID(ctx, id); err != nil {
			return nil, err
		}
		return nil, ErrInvalidStatus
	}

	return r.GetByID(ctx, id)
}

// GetByUserID retrieves all orders for a specific user with pagination
func (r *repository) GetByUserID(ctx context.Context, userID string, page, pageSize int) ([]*Order, int, error) {
	return r.List(ctx, page, pageSize, userID, 0)
}

// Close closes the database connection
func (r *repository) Close() error {
	return r.db.Close()
}

// priceOrder sets each item's subtotal (quantity * unit price) and the order total, all in integer
// cents. It refuses rather than wraps when a value would overflow int64.
func priceOrder(order *Order) error {
	var total int64
	for i := range order.Items {
		it := &order.Items[i]
		if it.Quantity < 0 || it.PriceCents < 0 {
			return fmt.Errorf("negative quantity or price for product %s", it.ProductID)
		}
		qty := int64(it.Quantity)
		if qty != 0 && it.PriceCents > math.MaxInt64/qty {
			return ErrTotalTooLarge
		}
		it.SubtotalCents = qty * it.PriceCents
		if total > math.MaxInt64-it.SubtotalCents {
			return ErrTotalTooLarge
		}
		total += it.SubtotalCents
	}
	order.TotalAmountCents = total
	return nil
}

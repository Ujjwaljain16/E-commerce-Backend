package order

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

var (
	// ErrOrderNotFound is returned when an order is not found
	ErrOrderNotFound = errors.New("order not found")
	// ErrInvalidStatus is returned when an invalid status transition is attempted
	ErrInvalidStatus = errors.New("invalid order status")
	// ErrEmptyOrderItems is returned when attempting to create an order without items
	ErrEmptyOrderItems = errors.New("order must have at least one item")
)

// OrderStatus represents the state of an order
type OrderStatus int

const (
	OrderStatusPending OrderStatus = 1
	OrderStatusConfirmed OrderStatus = 2
	OrderStatusProcessing OrderStatus = 3
	OrderStatusShipped OrderStatus = 4
	OrderStatusDelivered OrderStatus = 5
	OrderStatusCancelled OrderStatus = 6
	OrderStatusRefunded OrderStatus = 7
)

// OrderItem represents an item in an order
type OrderItem struct {
	ID          string
	OrderID     string
	ProductID   string
	ProductName string
	Quantity    int
	Price       float64
	Subtotal    float64
	CreatedAt   time.Time
}

// Order represents a customer order
type Order struct {
	ID              string
	UserID          string
	Status          OrderStatus
	Items           []OrderItem
	TotalAmount     float64
	ShippingAddress string
	BillingAddress  string
	PaymentMethod   string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Repository defines the interface for order data operations
type Repository interface {
	Create(ctx context.Context, order *Order) (*Order, error)
	GetByID(ctx context.Context, id string) (*Order, error)
	List(ctx context.Context, page, pageSize int, userID string, status OrderStatus) ([]*Order, int, error)
	UpdateStatus(ctx context.Context, id string, status OrderStatus) (*Order, error)
	Cancel(ctx context.Context, id string) error
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

	// Start transaction
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Generate order ID
	order.ID = uuid.New().String()
	order.CreatedAt = time.Now()
	order.UpdatedAt = time.Now()
	order.Status = OrderStatusPending

	// Calculate total amount
	order.TotalAmount = 0
	for i := range order.Items {
		order.Items[i].Subtotal = float64(order.Items[i].Quantity) * order.Items[i].Price
		order.TotalAmount += order.Items[i].Subtotal
	}

	// Insert order
	orderQuery := `
		INSERT INTO orders (id, user_id, status, total_amount, shipping_address, billing_address, payment_method, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err = tx.ExecContext(ctx, orderQuery,
		order.ID,
		order.UserID,
		order.Status,
		order.TotalAmount,
		order.ShippingAddress,
		order.BillingAddress,
		order.PaymentMethod,
		order.CreatedAt,
		order.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert order: %w", err)
	}

	// Insert order items
	itemQuery := `
		INSERT INTO order_items (id, order_id, product_id, product_name, quantity, price, subtotal, created_at)
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
			order.Items[i].Price,
			order.Items[i].Subtotal,
			order.Items[i].CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to insert order item: %w", err)
		}
	}

	// Commit transaction
	if err = tx.Commit(); err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	return order, nil
}

// GetByID retrieves an order by ID with its items
func (r *repository) GetByID(ctx context.Context, id string) (*Order, error) {
	// Get order
	orderQuery := `
		SELECT id, user_id, status, total_amount, shipping_address, billing_address, payment_method, created_at, updated_at
		FROM orders
		WHERE id = $1
	`
	order := &Order{}
	err := r.db.QueryRowContext(ctx, orderQuery, id).Scan(
		&order.ID,
		&order.UserID,
		&order.Status,
		&order.TotalAmount,
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
		SELECT id, order_id, product_id, product_name, quantity, price, subtotal, created_at
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
			&item.Price,
			&item.Subtotal,
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
		SELECT id, user_id, status, total_amount, shipping_address, billing_address, payment_method, created_at, updated_at
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
			&order.TotalAmount,
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
		SELECT id, order_id, product_id, product_name, quantity, price, subtotal, created_at
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
			&item.Price,
			&item.Subtotal,
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

// UpdateStatus updates the status of an order
func (r *repository) UpdateStatus(ctx context.Context, id string, status OrderStatus) (*Order, error) {
	query := `
		UPDATE orders
		SET status = $1, updated_at = $2
		WHERE id = $3
	`
	result, err := r.db.ExecContext(ctx, query, status, time.Now(), id)
	if err != nil {
		return nil, fmt.Errorf("failed to update order status: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return nil, ErrOrderNotFound
	}

	return r.GetByID(ctx, id)
}

// Cancel cancels an order by setting its status to cancelled
func (r *repository) Cancel(ctx context.Context, id string) error {
	// Check if order exists and is not already in a final state
	order, err := r.GetByID(ctx, id)
	if err != nil {
		return err
	}

	if order.Status == OrderStatusCancelled || order.Status == OrderStatusDelivered || order.Status == OrderStatusRefunded {
		return ErrInvalidStatus
	}

	query := `
		UPDATE orders
		SET status = $1, updated_at = $2
		WHERE id = $3
	`
	_, err = r.db.ExecContext(ctx, query, OrderStatusCancelled, time.Now(), id)
	if err != nil {
		return fmt.Errorf("failed to cancel order: %w", err)
	}

	return nil
}

// GetByUserID retrieves all orders for a specific user with pagination
func (r *repository) GetByUserID(ctx context.Context, userID string, page, pageSize int) ([]*Order, int, error) {
	return r.List(ctx, page, pageSize, userID, 0)
}

// Close closes the database connection
func (r *repository) Close() error {
	return r.db.Close()
}

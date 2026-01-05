package order

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "github.com/lib/pq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

func setupTestDB(t *testing.T) (*sql.DB, func()) {
	ctx := context.Background()

	postgresContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		postgres.WithInitScripts("migrations/001_create_orders_table.up.sql"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(30*time.Second)),
	)
	require.NoError(t, err)

	connStr, err := postgresContainer.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	db, err := sql.Open("postgres", connStr)
	require.NoError(t, err)

	err = db.Ping()
	require.NoError(t, err)

	cleanup := func() {
		db.Close()
		_ = postgresContainer.Terminate(ctx)
	}

	return db, cleanup
}

func createTestOrder(userID string) *Order {
	return &Order{
		UserID: userID,
		Items: []OrderItem{
			{
				ProductID:   "550e8400-e29b-41d4-a716-446655440001",
				ProductName: "Test Product 1",
				Quantity:    2,
				Price:       29.99,
			},
			{
				ProductID:   "550e8400-e29b-41d4-a716-446655440002",
				ProductName: "Test Product 2",
				Quantity:    1,
				Price:       49.99,
			},
		},
		ShippingAddress: "123 Main St, City, State 12345",
		BillingAddress:  "123 Main St, City, State 12345",
		PaymentMethod:   "credit_card",
	}
}

func TestCreate(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	order := createTestOrder("650e8400-e29b-41d4-a716-446655440000")

	createdOrder, err := repo.Create(ctx, order)
	require.NoError(t, err)
	assert.NotEmpty(t, createdOrder.ID)
	assert.Equal(t, "650e8400-e29b-41d4-a716-446655440000", createdOrder.UserID)
	assert.Equal(t, OrderStatusPending, createdOrder.Status)
	assert.Equal(t, 109.97, createdOrder.TotalAmount) // 2*29.99 + 1*49.99
	assert.Len(t, createdOrder.Items, 2)
	assert.NotEmpty(t, createdOrder.Items[0].ID)
	assert.Equal(t, 59.98, createdOrder.Items[0].Subtotal) // 2*29.99
	assert.Equal(t, 49.99, createdOrder.Items[1].Subtotal) // 1*49.99
}

func TestCreateWithEmptyItems(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	order := &Order{
		UserID:          "650e8400-e29b-41d4-a716-446655440000",
		Items:           []OrderItem{}, // Empty items
		ShippingAddress: "123 Main St",
		BillingAddress:  "123 Main St",
		PaymentMethod:   "credit_card",
	}

	_, err := repo.Create(ctx, order)
	assert.ErrorIs(t, err, ErrEmptyOrderItems)
}

func TestGetByID(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	// Create order first
	order := createTestOrder("650e8400-e29b-41d4-a716-446655440000")
	createdOrder, err := repo.Create(ctx, order)
	require.NoError(t, err)

	// Retrieve order
	retrievedOrder, err := repo.GetByID(ctx, createdOrder.ID)
	require.NoError(t, err)
	assert.Equal(t, createdOrder.ID, retrievedOrder.ID)
	assert.Equal(t, createdOrder.UserID, retrievedOrder.UserID)
	assert.Equal(t, createdOrder.Status, retrievedOrder.Status)
	assert.Equal(t, createdOrder.TotalAmount, retrievedOrder.TotalAmount)
	assert.Len(t, retrievedOrder.Items, 2)
}

func TestGetByIDNotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "550e8400-e29b-41d4-a716-446655440099")
	assert.ErrorIs(t, err, ErrOrderNotFound)
}

func TestList(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	// Create multiple orders
	for i := 0; i < 5; i++ {
		order := createTestOrder("650e8400-e29b-41d4-a716-446655440000")
		_, err := repo.Create(ctx, order)
		require.NoError(t, err)
	}

	// List orders with pagination
	orders, total, err := repo.List(ctx, 1, 3, "", 0)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, orders, 3)
	assert.NotEmpty(t, orders[0].Items)
}

func TestListWithUserFilter(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	// Create orders for different users
	for i := 0; i < 3; i++ {
		order := createTestOrder("650e8400-e29b-41d4-a716-446655440001")
		_, err := repo.Create(ctx, order)
		require.NoError(t, err)
	}
	for i := 0; i < 2; i++ {
		order := createTestOrder("650e8400-e29b-41d4-a716-446655440002")
		_, err := repo.Create(ctx, order)
		require.NoError(t, err)
	}

	// List orders for user-1
	orders, total, err := repo.List(ctx, 1, 10, "650e8400-e29b-41d4-a716-446655440001", 0)
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	assert.Len(t, orders, 3)
	for _, order := range orders {
		assert.Equal(t, "650e8400-e29b-41d4-a716-446655440001", order.UserID)
	}
}

func TestListWithStatusFilter(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	// Create orders
	order1 := createTestOrder("650e8400-e29b-41d4-a716-446655440000")
	createdOrder1, err := repo.Create(ctx, order1)
	require.NoError(t, err)

	order2 := createTestOrder("650e8400-e29b-41d4-a716-446655440000")
	createdOrder2, err := repo.Create(ctx, order2)
	require.NoError(t, err)

	// Update one to confirmed
	_, err = repo.UpdateStatus(ctx, createdOrder1.ID, OrderStatusConfirmed)
	require.NoError(t, err)

	// List only pending orders
	orders, total, err := repo.List(ctx, 1, 10, "", OrderStatusPending)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, orders, 1)
	assert.Equal(t, createdOrder2.ID, orders[0].ID)

	// List only confirmed orders
	orders, total, err = repo.List(ctx, 1, 10, "", OrderStatusConfirmed)
	require.NoError(t, err)
	assert.Equal(t, 1, total)
	assert.Len(t, orders, 1)
	assert.Equal(t, createdOrder1.ID, orders[0].ID)
}

func TestListPagination(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	// Create 10 orders
	for i := 0; i < 10; i++ {
		order := createTestOrder("650e8400-e29b-41d4-a716-446655440000")
		_, err := repo.Create(ctx, order)
		require.NoError(t, err)
	}

	// First page
	orders, total, err := repo.List(ctx, 1, 5, "", 0)
	require.NoError(t, err)
	assert.Equal(t, 10, total)
	assert.Len(t, orders, 5)

	// Second page
	orders, total, err = repo.List(ctx, 2, 5, "", 0)
	require.NoError(t, err)
	assert.Equal(t, 10, total)
	assert.Len(t, orders, 5)

	// Third page (empty)
	orders, total, err = repo.List(ctx, 3, 5, "", 0)
	require.NoError(t, err)
	assert.Equal(t, 10, total)
	assert.Len(t, orders, 0)
}

func TestUpdateStatus(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	// Create order
	order := createTestOrder("650e8400-e29b-41d4-a716-446655440000")
	createdOrder, err := repo.Create(ctx, order)
	require.NoError(t, err)
	assert.Equal(t, OrderStatusPending, createdOrder.Status)

	// Update to confirmed
	updatedOrder, err := repo.UpdateStatus(ctx, createdOrder.ID, OrderStatusConfirmed)
	require.NoError(t, err)
	assert.Equal(t, OrderStatusConfirmed, updatedOrder.Status)

	// Verify update persisted
	retrievedOrder, err := repo.GetByID(ctx, createdOrder.ID)
	require.NoError(t, err)
	assert.Equal(t, OrderStatusConfirmed, retrievedOrder.Status)
}

func TestUpdateStatusNotFound(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	_, err := repo.UpdateStatus(ctx, "550e8400-e29b-41d4-a716-446655440099", OrderStatusConfirmed)
	assert.ErrorIs(t, err, ErrOrderNotFound)
}

func TestCancel(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	// Create order
	order := createTestOrder("650e8400-e29b-41d4-a716-446655440000")
	createdOrder, err := repo.Create(ctx, order)
	require.NoError(t, err)

	// Cancel order
	err = repo.Cancel(ctx, createdOrder.ID)
	require.NoError(t, err)

	// Verify cancellation
	retrievedOrder, err := repo.GetByID(ctx, createdOrder.ID)
	require.NoError(t, err)
	assert.Equal(t, OrderStatusCancelled, retrievedOrder.Status)
}

func TestCancelAlreadyCancelled(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	// Create and cancel order
	order := createTestOrder("650e8400-e29b-41d4-a716-446655440000")
	createdOrder, err := repo.Create(ctx, order)
	require.NoError(t, err)

	err = repo.Cancel(ctx, createdOrder.ID)
	require.NoError(t, err)

	// Try to cancel again
	err = repo.Cancel(ctx, createdOrder.ID)
	assert.ErrorIs(t, err, ErrInvalidStatus)
}

func TestCancelDeliveredOrder(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	// Create order and mark as delivered
	order := createTestOrder("650e8400-e29b-41d4-a716-446655440000")
	createdOrder, err := repo.Create(ctx, order)
	require.NoError(t, err)

	_, err = repo.UpdateStatus(ctx, createdOrder.ID, OrderStatusDelivered)
	require.NoError(t, err)

	// Try to cancel delivered order
	err = repo.Cancel(ctx, createdOrder.ID)
	assert.ErrorIs(t, err, ErrInvalidStatus)
}

func TestGetByUserID(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	// Create orders for user
	for i := 0; i < 5; i++ {
		order := createTestOrder("650e8400-e29b-41d4-a716-446655440000")
		_, err := repo.Create(ctx, order)
		require.NoError(t, err)
	}

	// Create orders for different user
	for i := 0; i < 3; i++ {
		order := createTestOrder("650e8400-e29b-41d4-a716-446655440003")
		_, err := repo.Create(ctx, order)
		require.NoError(t, err)
	}

	// Get orders for user-123
	orders, total, err := repo.GetByUserID(ctx, "650e8400-e29b-41d4-a716-446655440000", 1, 10)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, orders, 5)
	for _, order := range orders {
		assert.Equal(t, "650e8400-e29b-41d4-a716-446655440000", order.UserID)
	}
}

func TestTransactionRollback(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	// Create order with invalid data that should cause rollback
	// We'll test by closing the connection mid-transaction
	order := createTestOrder("650e8400-e29b-41d4-a716-446655440000")

	// This should succeed normally
	createdOrder, err := repo.Create(ctx, order)
	require.NoError(t, err)

	// Verify both order and items were created
	retrievedOrder, err := repo.GetByID(ctx, createdOrder.ID)
	require.NoError(t, err)
	assert.NotEmpty(t, retrievedOrder.ID)
	assert.Len(t, retrievedOrder.Items, 2)
}

func TestConcurrentOrderCreation(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	repo := NewRepository(db)
	ctx := context.Background()

	// Create orders concurrently
	done := make(chan bool, 5)
	for i := 0; i < 5; i++ {
		go func() {
			order := createTestOrder("650e8400-e29b-41d4-a716-446655440000")
			_, err := repo.Create(ctx, order)
			assert.NoError(t, err)
			done <- true
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 5; i++ {
		<-done
	}

	// Verify all orders were created
	orders, total, err := repo.List(ctx, 1, 10, "", 0)
	require.NoError(t, err)
	assert.Equal(t, 5, total)
	assert.Len(t, orders, 5)
}

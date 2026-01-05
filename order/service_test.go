package order

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/order/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MockRepository is a mock implementation of Repository for testing
type MockRepository struct {
	CreateFunc       func(ctx context.Context, order *Order) (*Order, error)
	GetByIDFunc      func(ctx context.Context, id string) (*Order, error)
	ListFunc         func(ctx context.Context, page, pageSize int, userID string, status OrderStatus) ([]*Order, int, error)
	UpdateStatusFunc func(ctx context.Context, id string, status OrderStatus) (*Order, error)
	CancelFunc       func(ctx context.Context, id string) error
	GetByUserIDFunc  func(ctx context.Context, userID string, page, pageSize int) ([]*Order, int, error)
	CloseFunc        func() error
}

func (m *MockRepository) Create(ctx context.Context, order *Order) (*Order, error) {
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, order)
	}
	return nil, errors.New("not implemented")
}

func (m *MockRepository) GetByID(ctx context.Context, id string) (*Order, error) {
	if m.GetByIDFunc != nil {
		return m.GetByIDFunc(ctx, id)
	}
	return nil, errors.New("not implemented")
}

func (m *MockRepository) List(ctx context.Context, page, pageSize int, userID string, status OrderStatus) ([]*Order, int, error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, page, pageSize, userID, status)
	}
	return nil, 0, errors.New("not implemented")
}

func (m *MockRepository) UpdateStatus(ctx context.Context, id string, status OrderStatus) (*Order, error) {
	if m.UpdateStatusFunc != nil {
		return m.UpdateStatusFunc(ctx, id, status)
	}
	return nil, errors.New("not implemented")
}

func (m *MockRepository) Cancel(ctx context.Context, id string) error {
	if m.CancelFunc != nil {
		return m.CancelFunc(ctx, id)
	}
	return errors.New("not implemented")
}

func (m *MockRepository) GetByUserID(ctx context.Context, userID string, page, pageSize int) ([]*Order, int, error) {
	if m.GetByUserIDFunc != nil {
		return m.GetByUserIDFunc(ctx, userID, page, pageSize)
	}
	return nil, 0, errors.New("not implemented")
}

func (m *MockRepository) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

func setupService(repo Repository) *Service {
	log := logger.New("order-test")
	return NewService(repo, log)
}

func TestCreateOrder_Success(t *testing.T) {
	mockRepo := &MockRepository{
		CreateFunc: func(ctx context.Context, order *Order) (*Order, error) {
			order.ID = "test-order-id"
			order.CreatedAt = time.Now()
			order.UpdatedAt = time.Now()
			return order, nil
		},
	}

	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.CreateOrderRequest{
		UserId: "user-123",
		Items: []*pb.OrderItemInput{
			{ProductId: "prod-1", ProductName: "Product 1", Quantity: 1, Price: 10.0},
		},
		ShippingAddress: "123 Main St",
		BillingAddress:  "123 Main St",
		PaymentMethod:   "credit_card",
	}

	resp, err := service.CreateOrder(ctx, req)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if resp == nil {
		t.Fatal("Expected response, got nil")
	}

	if resp.Order.Id != "test-order-id" {
		t.Errorf("Expected ID test-order-id, got %s", resp.Order.Id)
	}
}

func TestCreateOrder_MissingUserID(t *testing.T) {
	mockRepo := &MockRepository{}
	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.CreateOrderRequest{
		UserId: "",
		Items: []*pb.OrderItemInput{
			{ProductId: "prod-1", Quantity: 1, Price: 10.0},
		},
	}

	_, err := service.CreateOrder(ctx, req)

	if err == nil {
		t.Error("Expected error, got nil")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.InvalidArgument {
		t.Errorf("Expected InvalidArgument error, got %v", err)
	}
}

func TestGetOrder_Success(t *testing.T) {
	mockRepo := &MockRepository{
		GetByIDFunc: func(ctx context.Context, id string) (*Order, error) {
			return &Order{
				ID:     id,
				UserID: "user-123",
				Status: OrderStatusPending,
				Items: []OrderItem{
					{ID: "item-1", ProductID: "prod-1", Quantity: 1, Price: 10.0},
				},
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}, nil
		},
	}

	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.GetOrderRequest{Id: "test-order-id"}
	resp, err := service.GetOrder(ctx, req)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if resp == nil {
		t.Fatal("Expected response, got nil")
	}

	if resp.Order.Id != "test-order-id" {
		t.Errorf("Expected ID test-order-id, got %s", resp.Order.Id)
	}
}

func TestGetOrder_NotFound(t *testing.T) {
	mockRepo := &MockRepository{
		GetByIDFunc: func(ctx context.Context, id string) (*Order, error) {
			return nil, ErrOrderNotFound
		},
	}

	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.GetOrderRequest{Id: "non-existent"}
	_, err := service.GetOrder(ctx, req)

	if err == nil {
		t.Error("Expected error, got nil")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.NotFound {
		t.Errorf("Expected NotFound error, got %v", err)
	}
}

func TestListOrders_Success(t *testing.T) {
	mockRepo := &MockRepository{
		ListFunc: func(ctx context.Context, page, pageSize int, userID string, status OrderStatus) ([]*Order, int, error) {
			return []*Order{
				{ID: "order-1", UserID: "user-1", Status: OrderStatusPending},
				{ID: "order-2", UserID: "user-2", Status: OrderStatusConfirmed},
			}, 2, nil
		},
	}

	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.ListOrdersRequest{
		Page:     1,
		PageSize: 10,
	}

	resp, err := service.ListOrders(ctx, req)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if len(resp.Orders) != 2 {
		t.Errorf("Expected 2 orders, got %d", len(resp.Orders))
	}
}

func TestUpdateOrderStatus_Success(t *testing.T) {
	mockRepo := &MockRepository{
		UpdateStatusFunc: func(ctx context.Context, id string, status OrderStatus) (*Order, error) {
			return &Order{
				ID:        id,
				Status:    status,
				UpdatedAt: time.Now(),
			}, nil
		},
	}

	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.UpdateOrderStatusRequest{
		Id:     "test-order-id",
		Status: pb.OrderStatus_CONFIRMED,
	}

	resp, err := service.UpdateOrderStatus(ctx, req)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if resp.Order.Status != pb.OrderStatus_CONFIRMED {
		t.Errorf("Expected status CONFIRMED, got %s", resp.Order.Status)
	}
}

func TestCancelOrder_Success(t *testing.T) {
	mockRepo := &MockRepository{
		CancelFunc: func(ctx context.Context, id string) error {
			return nil
		},
	}

	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.CancelOrderRequest{Id: "test-order-id"}
	resp, err := service.CancelOrder(ctx, req)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if !resp.Success {
		t.Error("Expected success to be true")
	}
}

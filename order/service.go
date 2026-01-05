package order

import (
	"context"

	"github.com/Ujjwaljain16/E-commerce-Backend/order/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Service implements the OrderService gRPC interface
type Service struct {
	pb.UnimplementedOrderServiceServer
	repo Repository
	log  *logger.Logger
}

// NewService creates a new order service
func NewService(repo Repository, log *logger.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log,
	}
}

// CreateOrder creates a new order with items
func (s *Service) CreateOrder(ctx context.Context, req *pb.CreateOrderRequest) (*pb.CreateOrderResponse, error) {
	// Validate input
	if req.UserId == "" {
		s.log.Warn(ctx, "Create order failed: user ID is required", nil)
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if len(req.Items) == 0 {
		s.log.Warn(ctx, "Create order failed: items are required", nil)
		return nil, status.Error(codes.InvalidArgument, "at least one item is required")
	}

	// Prepare order items
	var items []OrderItem
	for _, item := range req.Items {
		if item.ProductId == "" {
			return nil, status.Error(codes.InvalidArgument, "product_id is required for all items")
		}
		if item.Quantity <= 0 {
			return nil, status.Error(codes.InvalidArgument, "quantity must be positive for all items")
		}
		if item.Price < 0 {
			return nil, status.Error(codes.InvalidArgument, "price cannot be negative for all items")
		}

		items = append(items, OrderItem{
			ProductID:   item.ProductId,
			ProductName: item.ProductName,
			Quantity:    int(item.Quantity),
			Price:       item.Price,
		})
	}

	// Create order object
	order := &Order{
		UserID:          req.UserId,
		ShippingAddress: req.ShippingAddress,
		BillingAddress:  req.BillingAddress,
		PaymentMethod:   req.PaymentMethod,
		Items:           items,
	}

	// Save to repository
	created, err := s.repo.Create(ctx, order)
	if err != nil {
		s.log.Error(ctx, "Failed to create order", map[string]interface{}{"error": err.Error(), "user_id": req.UserId})
		return nil, status.Error(codes.Internal, "failed to create order")
	}

	s.log.Info(ctx, "Order created successfully", map[string]interface{}{"order_id": created.ID, "user_id": created.UserID})

	return &pb.CreateOrderResponse{
		Order: toProtoOrder(created),
	}, nil
}

// GetOrder retrieves an order by ID
func (s *Service) GetOrder(ctx context.Context, req *pb.GetOrderRequest) (*pb.GetOrderResponse, error) {
	if req.Id == "" {
		s.log.Warn(ctx, "Get order failed: ID is required", nil)
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	order, err := s.repo.GetByID(ctx, req.Id)
	if err != nil {
		if err == ErrOrderNotFound {
			s.log.Warn(ctx, "Order not found", map[string]interface{}{"order_id": req.Id})
			return nil, status.Error(codes.NotFound, "order not found")
		}
		s.log.Error(ctx, "Failed to get order", map[string]interface{}{"error": err.Error(), "order_id": req.Id})
		return nil, status.Error(codes.Internal, "failed to get order")
	}

	return &pb.GetOrderResponse{
		Order: toProtoOrder(order),
	}, nil
}

// ListOrders retrieves orders with pagination and optional filters
func (s *Service) ListOrders(ctx context.Context, req *pb.ListOrdersRequest) (*pb.ListOrdersResponse, error) {
	page := int(req.Page)
	if page < 1 {
		page = 1
	}

	pageSize := int(req.PageSize)
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}

	statusFilter := OrderStatus(0)
	if req.Status != pb.OrderStatus_ORDER_STATUS_UNSPECIFIED {
		statusFilter = OrderStatus(req.Status)
	}

	orders, total, err := s.repo.List(ctx, page, pageSize, req.UserId, statusFilter)
	if err != nil {
		s.log.Error(ctx, "Failed to list orders", map[string]interface{}{"error": err.Error()})
		return nil, status.Error(codes.Internal, "failed to list orders")
	}

	protoOrders := make([]*pb.Order, len(orders))
	for i, o := range orders {
		protoOrders[i] = toProtoOrder(o)
	}

	return &pb.ListOrdersResponse{
		Orders:   protoOrders,
		Total:    int32(total),
		Page:     int32(page),
		PageSize: int32(pageSize),
	}, nil
}

// UpdateOrderStatus updates the status of an order
func (s *Service) UpdateOrderStatus(ctx context.Context, req *pb.UpdateOrderStatusRequest) (*pb.UpdateOrderStatusResponse, error) {
	if req.Id == "" {
		s.log.Warn(ctx, "Update order status failed: ID is required", nil)
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	if req.Status == pb.OrderStatus_ORDER_STATUS_UNSPECIFIED {
		return nil, status.Error(codes.InvalidArgument, "status is required")
	}

	updated, err := s.repo.UpdateStatus(ctx, req.Id, OrderStatus(req.Status))
	if err != nil {
		if err == ErrOrderNotFound {
			s.log.Warn(ctx, "Order not found for status update", map[string]interface{}{"order_id": req.Id})
			return nil, status.Error(codes.NotFound, "order not found")
		}
		s.log.Error(ctx, "Failed to update order status", map[string]interface{}{"error": err.Error(), "order_id": req.Id})
		return nil, status.Error(codes.Internal, "failed to update order status")
	}

	s.log.Info(ctx, "Order status updated successfully", map[string]interface{}{
		"order_id": updated.ID,
		"status":   updated.Status,
	})

	return &pb.UpdateOrderStatusResponse{
		Order: toProtoOrder(updated),
	}, nil
}

// CancelOrder cancels an order
func (s *Service) CancelOrder(ctx context.Context, req *pb.CancelOrderRequest) (*pb.CancelOrderResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	err := s.repo.Cancel(ctx, req.Id)
	if err != nil {
		if err == ErrOrderNotFound {
			return nil, status.Error(codes.NotFound, "order not found")
		}
		if err == ErrInvalidStatus {
			return nil, status.Error(codes.FailedPrecondition, "order cannot be cancelled in its current state")
		}
		s.log.Error(ctx, "Failed to cancel order", map[string]interface{}{"error": err.Error(), "order_id": req.Id})
		return nil, status.Error(codes.Internal, "failed to cancel order")
	}

	s.log.Info(ctx, "Order cancelled successfully", map[string]interface{}{"order_id": req.Id, "reason": req.Reason})

	return &pb.CancelOrderResponse{
		Success: true,
		Message: "Order cancelled successfully",
	}, nil
}

// GetOrdersByUser retrieves all orders for a specific user
func (s *Service) GetOrdersByUser(ctx context.Context, req *pb.GetOrdersByUserRequest) (*pb.GetOrdersByUserResponse, error) {
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}

	page := int(req.Page)
	if page < 1 {
		page = 1
	}

	pageSize := int(req.PageSize)
	if pageSize < 1 {
		pageSize = 10
	}

	orders, total, err := s.repo.GetByUserID(ctx, req.UserId, page, pageSize)
	if err != nil {
		s.log.Error(ctx, "Failed to get user orders", map[string]interface{}{"error": err.Error(), "user_id": req.UserId})
		return nil, status.Error(codes.Internal, "failed to get user orders")
	}

	protoOrders := make([]*pb.Order, len(orders))
	for i, o := range orders {
		protoOrders[i] = toProtoOrder(o)
	}

	return &pb.GetOrdersByUserResponse{
		Orders:   protoOrders,
		Total:    int32(total),
		Page:     int32(page),
		PageSize: int32(pageSize),
	}, nil
}

// toProtoOrder converts a domain Order to a protobuf Order
func toProtoOrder(o *Order) *pb.Order {
	if o == nil {
		return nil
	}

	items := make([]*pb.OrderItem, len(o.Items))
	for i, item := range o.Items {
		items[i] = &pb.OrderItem{
			Id:          item.ID,
			OrderId:     item.OrderID,
			ProductId:   item.ProductID,
			ProductName: item.ProductName,
			Quantity:    int32(item.Quantity),
			Price:       item.Price,
			Subtotal:    item.Subtotal,
		}
	}

	return &pb.Order{
		Id:              o.ID,
		UserId:          o.UserID,
		Status:          pb.OrderStatus(o.Status),
		Items:           items,
		TotalAmount:     o.TotalAmount,
		ShippingAddress: o.ShippingAddress,
		BillingAddress:  o.BillingAddress,
		PaymentMethod:   o.PaymentMethod,
		CreatedAt:       timestamppb.New(o.CreatedAt),
		UpdatedAt:       timestamppb.New(o.UpdatedAt),
	}
}

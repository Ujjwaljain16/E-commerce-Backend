package order

import (
	"context"
	"errors"
	"math"

	catalogpb "github.com/Ujjwaljain16/E-commerce-Backend/catalog/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/order/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Inventory is the part of the catalog service the order service needs.
type Inventory interface {
	ReserveStock(ctx context.Context, in *catalogpb.ReserveStockRequest, opts ...grpc.CallOption) (*catalogpb.ReserveStockResponse, error)
	ReleaseStock(ctx context.Context, in *catalogpb.ReleaseStockRequest, opts ...grpc.CallOption) (*catalogpb.ReleaseStockResponse, error)
}

// Service implements the OrderService gRPC interface
type Service struct {
	pb.UnimplementedOrderServiceServer
	repo      Repository
	inventory Inventory
	log       *logger.Logger
}

// NewService creates a new order service
func NewService(repo Repository, inventory Inventory, log *logger.Logger) *Service {
	return &Service{
		repo:      repo,
		inventory: inventory,
		log:       log,
	}
}

// CreateOrder reserves stock in the catalog, prices the items from the catalog (client prices are
// ignored) and stores the order together with its order.created event.
func (s *Service) CreateOrder(ctx context.Context, req *pb.CreateOrderRequest) (*pb.CreateOrderResponse, error) {
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if _, err := uuid.Parse(req.UserId); err != nil {
		return nil, status.Error(codes.InvalidArgument, "user_id must be a UUID")
	}
	if len(req.Items) == 0 {
		return nil, status.Error(codes.InvalidArgument, "at least one item is required")
	}
	if len(req.Items) > 100 {
		return nil, status.Error(codes.InvalidArgument, "an order can have at most 100 items")
	}
	if req.ShippingAddress == "" || req.PaymentMethod == "" {
		return nil, status.Error(codes.InvalidArgument, "shipping_address and payment_method are required")
	}
	billing := req.BillingAddress
	if billing == "" {
		billing = req.ShippingAddress
	}

	stock := make([]*catalogpb.StockItem, 0, len(req.Items))
	for _, item := range req.Items {
		if item.ProductId == "" {
			return nil, status.Error(codes.InvalidArgument, "product_id is required for all items")
		}
		if item.Quantity <= 0 {
			return nil, status.Error(codes.InvalidArgument, "quantity must be positive for all items")
		}
		stock = append(stock, &catalogpb.StockItem{ProductId: item.ProductId, Quantity: item.Quantity})
	}

	orderID := uuid.NewString()
	reserved, err := s.inventory.ReserveStock(ctx, &catalogpb.ReserveStockRequest{OrderId: orderID, Items: stock})
	if err != nil {
		switch status.Code(err) {
		case codes.InvalidArgument, codes.NotFound, codes.FailedPrecondition:
			return nil, err
		}
		s.log.Error(ctx, "Failed to reserve stock", map[string]interface{}{"error": err.Error(), "user_id": req.UserId})
		return nil, status.Error(codes.Unavailable, "could not reserve stock, try again")
	}

	items := make([]OrderItem, 0, len(reserved.Items))
	for _, it := range reserved.Items {
		items = append(items, OrderItem{
			ProductID:   it.ProductId,
			ProductName: it.ProductName,
			Quantity:    int(it.Quantity),
			Price:       it.UnitPrice,
		})
	}

	created, err := s.repo.Create(ctx, &Order{
		ID:              orderID,
		UserID:          req.UserId,
		ShippingAddress: req.ShippingAddress,
		BillingAddress:  billing,
		PaymentMethod:   req.PaymentMethod,
		Items:           items,
	})
	if err != nil {
		s.log.Error(ctx, "Failed to create order", map[string]interface{}{"error": err.Error(), "user_id": req.UserId})
		// No order row exists, so no event will ever release this reservation; do it now.
		if _, rerr := s.inventory.ReleaseStock(context.WithoutCancel(ctx), &catalogpb.ReleaseStockRequest{OrderId: orderID}); rerr != nil {
			s.log.Error(ctx, "Failed to release stock after order failure", map[string]interface{}{"error": rerr.Error(), "order_id": orderID})
		}
		return nil, status.Error(codes.Internal, "failed to create order")
	}
	created.TotalAmount = math.Round(created.TotalAmount*100) / 100

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
		if errors.Is(err, ErrOrderNotFound) {
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

// UpdateOrderStatus moves an order along the fulfilment flow (CONFIRMED → PROCESSING → SHIPPED → DELIVERED).
func (s *Service) UpdateOrderStatus(ctx context.Context, req *pb.UpdateOrderStatusRequest) (*pb.UpdateOrderStatusResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	switch req.Status {
	case pb.OrderStatus_ORDER_STATUS_UNSPECIFIED:
		return nil, status.Error(codes.InvalidArgument, "status is required")
	case pb.OrderStatus_CANCELLED:
		order, err := s.cancel(ctx, req.Id, "cancelled by admin")
		if err != nil {
			return nil, err
		}
		return &pb.UpdateOrderStatusResponse{Order: toProtoOrder(order)}, nil
	case pb.OrderStatus_REFUNDED, pb.OrderStatus_PENDING:
		return nil, status.Error(codes.FailedPrecondition, "refunds go through the payment service; orders cannot return to PENDING")
	}

	updated, err := s.repo.UpdateStatus(ctx, req.Id, OrderStatus(req.Status))
	if err != nil {
		if errors.Is(err, ErrOrderNotFound) {
			return nil, status.Error(codes.NotFound, "order not found")
		}
		if errors.Is(err, ErrInvalidStatus) {
			return nil, status.Error(codes.FailedPrecondition, "order cannot move to that status from its current state")
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

// CancelOrder cancels a PENDING or CONFIRMED order. Stock release and refunds follow from the order.cancelled event.
func (s *Service) CancelOrder(ctx context.Context, req *pb.CancelOrderRequest) (*pb.CancelOrderResponse, error) {
	if req.Id == "" {
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}
	reason := req.Reason
	if reason == "" {
		reason = "cancelled by customer"
	}
	if _, err := s.cancel(ctx, req.Id, reason); err != nil {
		return nil, err
	}
	return &pb.CancelOrderResponse{
		Success: true,
		Message: "Order cancelled successfully",
	}, nil
}

func (s *Service) cancel(ctx context.Context, id, reason string) (*Order, error) {
	order, err := s.repo.Cancel(ctx, id, reason)
	if err != nil {
		if errors.Is(err, ErrOrderNotFound) {
			return nil, status.Error(codes.NotFound, "order not found")
		}
		if errors.Is(err, ErrInvalidStatus) {
			return nil, status.Error(codes.FailedPrecondition, "order cannot be cancelled in its current state")
		}
		s.log.Error(ctx, "Failed to cancel order", map[string]interface{}{"error": err.Error(), "order_id": id})
		return nil, status.Error(codes.Internal, "failed to cancel order")
	}
	s.log.Info(ctx, "Order cancelled", map[string]interface{}{"order_id": id, "reason": reason})
	return order, nil
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
	if pageSize > 100 {
		pageSize = 100
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

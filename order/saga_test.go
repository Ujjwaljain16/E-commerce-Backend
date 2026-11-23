package order

import (
	"context"
	"errors"
	"testing"

	catalogpb "github.com/Ujjwaljain16/E-commerce-Backend/catalog/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/order/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/events"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/kafka"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	kafkago "github.com/segmentio/kafka-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const testUserID = "3f2b6f0e-8d0c-4b7a-9b0e-6c1f2a3d4e5f"

type fakeInventory struct {
	reserveErr error
	reserved   *catalogpb.ReserveStockRequest
	released   []string
}

func (f *fakeInventory) ReserveStock(_ context.Context, in *catalogpb.ReserveStockRequest, _ ...grpc.CallOption) (*catalogpb.ReserveStockResponse, error) {
	f.reserved = in
	if f.reserveErr != nil {
		return nil, f.reserveErr
	}
	resp := &catalogpb.ReserveStockResponse{}
	for _, it := range in.Items {
		resp.Items = append(resp.Items, &catalogpb.ReservedItem{
			ProductId: it.ProductId, ProductName: "catalog name", Quantity: it.Quantity, UnitPriceCents: 2500,
		})
	}
	return resp, nil
}

func (f *fakeInventory) ReleaseStock(_ context.Context, in *catalogpb.ReleaseStockRequest, _ ...grpc.CallOption) (*catalogpb.ReleaseStockResponse, error) {
	f.released = append(f.released, in.OrderId)
	return &catalogpb.ReleaseStockResponse{Released: true}, nil
}

func validCreateRequest() *pb.CreateOrderRequest {
	return &pb.CreateOrderRequest{
		UserId:          testUserID,
		Items:           []*pb.OrderItemInput{{ProductId: "p1", Quantity: 2, PriceCents: 1, ProductName: "client says cheap"}},
		ShippingAddress: "1 Main St",
		PaymentMethod:   "card",
	}
}

func TestCreateOrder_UsesCatalogPricesNotClientPrices(t *testing.T) {
	inv := &fakeInventory{}
	var stored *Order
	repo := &MockRepository{CreateFunc: func(_ context.Context, o *Order) (*Order, error) {
		stored = o
		o.TotalAmountCents = int64(o.Items[0].Quantity) * o.Items[0].PriceCents
		return o, nil
	}}
	svc := NewService(repo, inv, logger.New("test"))

	resp, err := svc.CreateOrder(context.Background(), validCreateRequest())
	if err != nil {
		t.Fatal(err)
	}
	if stored.Items[0].PriceCents != 2500 || stored.Items[0].ProductName != "catalog name" {
		t.Fatalf("expected catalog price/name, got %+v", stored.Items[0])
	}
	if resp.Order.TotalAmountCents != 5000 {
		t.Fatalf("expected total 5000, got %v", resp.Order.TotalAmountCents)
	}
	if inv.reserved.OrderId != stored.ID || stored.BillingAddress != "1 Main St" {
		t.Fatalf("reservation and order should share an id and billing should default to shipping")
	}
}

func TestCreateOrder_PassesThroughStockErrors(t *testing.T) {
	inv := &fakeInventory{reserveErr: status.Error(codes.FailedPrecondition, "insufficient stock")}
	svc := NewService(&MockRepository{}, inv, logger.New("test"))
	if _, err := svc.CreateOrder(context.Background(), validCreateRequest()); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected FailedPrecondition, got %v", err)
	}
}

func TestCreateOrder_ReleasesStockWhenSaveFails(t *testing.T) {
	inv := &fakeInventory{}
	repo := &MockRepository{CreateFunc: func(context.Context, *Order) (*Order, error) { return nil, errors.New("db down") }}
	svc := NewService(repo, inv, logger.New("test"))
	if _, err := svc.CreateOrder(context.Background(), validCreateRequest()); status.Code(err) != codes.Internal {
		t.Fatalf("expected Internal, got %v", err)
	}
	if len(inv.released) != 1 || inv.released[0] != inv.reserved.OrderId {
		t.Fatalf("expected the reservation to be released, got %v", inv.released)
	}
}

func TestCreateOrder_Validation(t *testing.T) {
	svc := NewService(&MockRepository{}, &fakeInventory{}, logger.New("test"))
	bad := []func(r *pb.CreateOrderRequest){
		func(r *pb.CreateOrderRequest) { r.UserId = "not-a-uuid" },
		func(r *pb.CreateOrderRequest) { r.Items = nil },
		func(r *pb.CreateOrderRequest) { r.Items[0].Quantity = 0 },
		func(r *pb.CreateOrderRequest) { r.ShippingAddress = "" },
		func(r *pb.CreateOrderRequest) { r.PaymentMethod = "" },
	}
	for i, mutate := range bad {
		req := validCreateRequest()
		mutate(req)
		if _, err := svc.CreateOrder(context.Background(), req); status.Code(err) != codes.InvalidArgument {
			t.Errorf("case %d: expected InvalidArgument, got %v", i, err)
		}
	}
}

func TestUpdateOrderStatus_RejectsRefundAndMapsInvalidTransition(t *testing.T) {
	repo := &MockRepository{UpdateStatusFunc: func(context.Context, string, OrderStatus) (*Order, error) {
		return nil, ErrInvalidStatus
	}}
	svc := setupService(repo)
	ctx := context.Background()
	if _, err := svc.UpdateOrderStatus(ctx, &pb.UpdateOrderStatusRequest{Id: "x", Status: pb.OrderStatus_REFUNDED}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected FailedPrecondition for REFUNDED, got %v", err)
	}
	if _, err := svc.UpdateOrderStatus(ctx, &pb.UpdateOrderStatusRequest{Id: "x", Status: pb.OrderStatus_DELIVERED}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected FailedPrecondition for bad transition, got %v", err)
	}
}

func TestCancelOrder_NotCancellable(t *testing.T) {
	repo := &MockRepository{CancelFunc: func(context.Context, string, string) (*Order, error) { return nil, ErrInvalidStatus }}
	if _, err := setupService(repo).CancelOrder(context.Background(), &pb.CancelOrderRequest{Id: "x"}); status.Code(err) != codes.FailedPrecondition {
		t.Fatalf("expected FailedPrecondition, got %v", err)
	}
}

type paymentRepo struct {
	MockRepository
	applied   []bool
	refunded  []string
	applyErr  error
	lastOrder string
}

func (r *paymentRepo) ApplyPaymentResult(_ context.Context, id string, ok bool, _ string) (bool, error) {
	r.lastOrder = id
	r.applied = append(r.applied, ok)
	return true, r.applyErr
}

func (r *paymentRepo) MarkRefunded(_ context.Context, id string) error {
	r.refunded = append(r.refunded, id)
	return nil
}

func TestPaymentEventHandler(t *testing.T) {
	repo := &paymentRepo{}
	h := NewPaymentEventHandler(repo, logger.New("test"))
	ctx := context.Background()
	payload := []byte(`{"order_id":"o1","payment_id":"p1","reason":"declined"}`)

	if err := h.Handle(ctx, kafkago.Message{Topic: events.TopicPaymentSucceeded, Value: payload}); err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(ctx, kafkago.Message{Topic: events.TopicPaymentFailed, Value: payload}); err != nil {
		t.Fatal(err)
	}
	if err := h.Handle(ctx, kafkago.Message{Topic: events.TopicPaymentRefunded, Value: payload}); err != nil {
		t.Fatal(err)
	}
	if len(repo.applied) != 2 || !repo.applied[0] || repo.applied[1] || repo.lastOrder != "o1" || len(repo.refunded) != 1 {
		t.Fatalf("unexpected calls: applied=%v refunded=%v", repo.applied, repo.refunded)
	}

	err := h.Handle(ctx, kafkago.Message{Topic: events.TopicPaymentSucceeded, Value: []byte("{not json")})
	if !errors.Is(err, kafka.ErrSkip) {
		t.Fatalf("malformed message should be skipped, got %v", err)
	}

	repo.applyErr = errors.New("db down")
	if err := h.Handle(ctx, kafkago.Message{Topic: events.TopicPaymentSucceeded, Value: payload}); err == nil || errors.Is(err, kafka.ErrSkip) {
		t.Fatalf("transient errors must be retried, got %v", err)
	}
}

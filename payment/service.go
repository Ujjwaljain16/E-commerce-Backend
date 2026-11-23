package payment

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Ujjwaljain16/E-commerce-Backend/payment/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/events"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

var (
	// ErrNotRefundable is returned when refunding a payment that has not succeeded.
	ErrNotRefundable = errors.New("only succeeded payments can be refunded")
	// ErrKeyReused is returned when an idempotency key already belongs to a different order.
	ErrKeyReused = errors.New("idempotency key already used for another order")
)

// invalidInputError is a request problem that retrying will never fix.
type invalidInputError struct{ msg string }

func (e invalidInputError) Error() string { return e.msg }

func invalid(format string, args ...interface{}) error {
	return invalidInputError{msg: fmt.Sprintf(format, args...)}
}

// IsInvalidInput reports whether err was caused by bad input rather than a failing dependency.
func IsInvalidInput(err error) bool {
	var inv invalidInputError
	return errors.As(err, &inv)
}

// Service implements the PaymentService gRPC interface and the order event handlers.
type Service struct {
	pb.UnimplementedPaymentServiceServer
	repo      Repository
	processor Processor
	log       *logger.Logger
}

// NewService creates a payment service.
func NewService(repo Repository, processor Processor, log *logger.Logger) *Service {
	return &Service{repo: repo, processor: processor, log: log}
}

// ProcessInput describes a charge for one order.
type ProcessInput struct {
	OrderID        string
	UserID         string
	AmountCents    int64
	Currency       string
	Method         string
	IdempotencyKey string
}

// Process charges an order at most once. Repeating a call with the same key (or for the same order)
// returns the payment that already exists.
func (s *Service) Process(ctx context.Context, in ProcessInput) (*Payment, error) {
	if err := requireUUID("order_id", in.OrderID); err != nil {
		return nil, err
	}
	if err := requireUUID("user_id", in.UserID); err != nil {
		return nil, err
	}
	if in.AmountCents <= 0 {
		return nil, invalid("amount_cents must be greater than zero")
	}
	currency := strings.ToUpper(strings.TrimSpace(in.Currency))
	if currency == "" {
		currency = "USD"
	}
	if len(currency) != 3 {
		return nil, invalid("currency must be a 3-letter code")
	}
	if len(in.Method) > 50 {
		return nil, invalid("method must be at most 50 characters")
	}
	key := strings.TrimSpace(in.IdempotencyKey)
	if key == "" {
		key = in.OrderID
	}
	if len(key) > 100 {
		return nil, invalid("idempotency_key must be at most 100 characters")
	}

	p, created, err := s.repo.Insert(ctx, &Payment{
		ID:             uuid.NewString(),
		OrderID:        in.OrderID,
		UserID:         in.UserID,
		AmountCents:    in.AmountCents,
		Currency:       currency,
		Status:         StatusPending,
		Method:         in.Method,
		IdempotencyKey: key,
	})
	if err != nil {
		return nil, err
	}
	if !created {
		if p.OrderID != in.OrderID {
			return nil, ErrKeyReused
		}
		if p.Status != StatusPending {
			return p, nil
		}
		// A PENDING row means an earlier attempt died mid-charge. The provider dedupes on the
		// idempotency key, so charging again is safe and keeps the payment from being stuck.
		s.log.Warn(ctx, "Resuming pending payment", map[string]interface{}{"payment_id": p.ID, "order_id": p.OrderID})
	}
	return s.charge(ctx, p)
}

func (s *Service) charge(ctx context.Context, p *Payment) (*Payment, error) {
	ref, err := s.processor.Charge(ctx, ChargeRequest{
		PaymentID:      p.ID,
		OrderID:        p.OrderID,
		AmountCents:    p.AmountCents,
		Currency:       p.Currency,
		Method:         p.Method,
		IdempotencyKey: p.IdempotencyKey,
	})

	t := Transition{ID: p.ID, From: StatusPending}
	var decline *DeclineError
	switch {
	case err == nil:
		t.To, t.ProviderRef, t.Topic = StatusSucceeded, ref, events.TopicPaymentSucceeded
		t.Event = paymentEvent(p, "")
	case errors.As(err, &decline):
		t.To, t.FailureReason, t.Topic = StatusFailed, decline.Reason, events.TopicPaymentFailed
		t.Event = paymentEvent(p, decline.Reason)
	default:
		return nil, fmt.Errorf("charge payment %s: %w", p.ID, err)
	}

	updated, err := s.repo.Apply(ctx, t)
	if errors.Is(err, ErrStatusChanged) {
		// A concurrent attempt for the same payment finished first; its result stands.
		return s.repo.GetByID(ctx, p.ID)
	}
	if err != nil {
		return nil, err
	}
	s.log.Info(ctx, "Payment processed", map[string]interface{}{
		"payment_id": updated.ID, "order_id": updated.OrderID, "status": pb.PaymentStatus(updated.Status).String(),
	})
	return updated, nil
}

// Refund reverses a succeeded payment.
func (s *Service) Refund(ctx context.Context, paymentID, reason string) (*Payment, error) {
	if err := requireUUID("payment_id", paymentID); err != nil {
		return nil, err
	}
	p, err := s.repo.GetByID(ctx, paymentID)
	if err != nil {
		return nil, err
	}
	if p.Status != StatusSucceeded {
		return nil, ErrNotRefundable
	}
	if err := s.processor.Refund(ctx, RefundRequest{
		ProviderRef: p.ProviderRef, AmountCents: p.AmountCents, Currency: p.Currency, Reason: reason,
	}); err != nil {
		return nil, fmt.Errorf("refund payment %s: %w", p.ID, err)
	}

	updated, err := s.repo.Apply(ctx, Transition{
		ID:    p.ID,
		From:  StatusSucceeded,
		To:    StatusRefunded,
		Topic: events.TopicPaymentRefunded,
		Event: paymentEvent(p, reason),
	})
	if errors.Is(err, ErrStatusChanged) {
		return nil, ErrNotRefundable
	}
	if err != nil {
		return nil, err
	}
	s.log.Info(ctx, "Payment refunded", map[string]interface{}{"payment_id": updated.ID, "order_id": updated.OrderID})
	return updated, nil
}

// CancelOrder reacts to an order being cancelled: a succeeded payment is refunded, and an order
// that was never charged gets a CANCELLED payment so a late order.created cannot charge it.
func (s *Service) CancelOrder(ctx context.Context, orderID, userID, reason string) error {
	if err := requireUUID("order_id", orderID); err != nil {
		return err
	}
	if err := requireUUID("user_id", userID); err != nil {
		return err
	}
	if reason == "" {
		reason = "order cancelled"
	}

	p, err := s.repo.GetByOrderID(ctx, orderID)
	if errors.Is(err, ErrPaymentNotFound) {
		var created bool
		p, created, err = s.repo.Insert(ctx, &Payment{
			ID:             uuid.NewString(),
			OrderID:        orderID,
			UserID:         userID,
			Currency:       "USD",
			Status:         StatusCancelled,
			FailureReason:  reason,
			IdempotencyKey: orderID,
		})
		if err != nil {
			return err
		}
		if created {
			s.log.Info(ctx, "Order cancelled before payment", map[string]interface{}{"order_id": orderID})
			return nil
		}
	} else if err != nil {
		return err
	}

	if p.Status != StatusSucceeded {
		return nil
	}
	if _, err := s.Refund(ctx, p.ID, reason); err != nil && !errors.Is(err, ErrNotRefundable) {
		return err
	}
	return nil
}

func paymentEvent(p *Payment, reason string) events.Payment {
	return events.Payment{
		Meta:        events.NewMeta(),
		PaymentID:   p.ID,
		OrderID:     p.OrderID,
		UserID:      p.UserID,
		AmountCents: p.AmountCents,
		Currency:    p.Currency,
		Reason:      reason,
	}
}

func requireUUID(field, value string) error {
	if value == "" {
		return invalid("%s is required", field)
	}
	if _, err := uuid.Parse(value); err != nil {
		return invalid("%s must be a valid UUID", field)
	}
	return nil
}

// ProcessPayment charges an order.
func (s *Service) ProcessPayment(ctx context.Context, req *pb.ProcessPaymentRequest) (*pb.ProcessPaymentResponse, error) {
	p, err := s.Process(ctx, ProcessInput{
		OrderID:        req.OrderId,
		UserID:         req.UserId,
		AmountCents:    req.AmountCents,
		Currency:       req.Currency,
		Method:         req.Method,
		IdempotencyKey: req.IdempotencyKey,
	})
	if err != nil {
		return nil, s.toStatus(ctx, "process payment", err)
	}
	return &pb.ProcessPaymentResponse{Payment: toProto(p)}, nil
}

// GetPayment returns a payment by ID.
func (s *Service) GetPayment(ctx context.Context, req *pb.GetPaymentRequest) (*pb.GetPaymentResponse, error) {
	if err := requireUUID("id", req.Id); err != nil {
		return nil, s.toStatus(ctx, "get payment", err)
	}
	p, err := s.repo.GetByID(ctx, req.Id)
	if err != nil {
		return nil, s.toStatus(ctx, "get payment", err)
	}
	return &pb.GetPaymentResponse{Payment: toProto(p)}, nil
}

// GetPaymentByOrder returns the payment for an order.
func (s *Service) GetPaymentByOrder(ctx context.Context, req *pb.GetPaymentByOrderRequest) (*pb.GetPaymentResponse, error) {
	if err := requireUUID("order_id", req.OrderId); err != nil {
		return nil, s.toStatus(ctx, "get payment", err)
	}
	p, err := s.repo.GetByOrderID(ctx, req.OrderId)
	if err != nil {
		return nil, s.toStatus(ctx, "get payment", err)
	}
	return &pb.GetPaymentResponse{Payment: toProto(p)}, nil
}

// ListPayments returns a user's payments, newest first.
func (s *Service) ListPayments(ctx context.Context, req *pb.ListPaymentsRequest) (*pb.ListPaymentsResponse, error) {
	if err := requireUUID("user_id", req.UserId); err != nil {
		return nil, s.toStatus(ctx, "list payments", err)
	}
	page, pageSize := clampPage(req.Page, req.PageSize)

	payments, total, err := s.repo.ListByUser(ctx, req.UserId, page, pageSize)
	if err != nil {
		return nil, s.toStatus(ctx, "list payments", err)
	}
	out := make([]*pb.Payment, len(payments))
	for i, p := range payments {
		out[i] = toProto(p)
	}
	return &pb.ListPaymentsResponse{
		Payments: out,
		Total:    int32(total),
		Page:     int32(page),
		PageSize: int32(pageSize),
	}, nil
}

// RefundPayment refunds a succeeded payment.
func (s *Service) RefundPayment(ctx context.Context, req *pb.RefundPaymentRequest) (*pb.RefundPaymentResponse, error) {
	p, err := s.Refund(ctx, req.PaymentId, req.Reason)
	if err != nil {
		return nil, s.toStatus(ctx, "refund payment", err)
	}
	return &pb.RefundPaymentResponse{Payment: toProto(p)}, nil
}

func (s *Service) toStatus(ctx context.Context, op string, err error) error {
	switch {
	case IsInvalidInput(err):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, ErrPaymentNotFound):
		return status.Error(codes.NotFound, "payment not found")
	case errors.Is(err, ErrNotRefundable):
		return status.Error(codes.FailedPrecondition, err.Error())
	case errors.Is(err, ErrKeyReused):
		return status.Error(codes.AlreadyExists, err.Error())
	default:
		s.log.Error(ctx, "Failed to "+op, map[string]interface{}{"error": err.Error()})
		return status.Error(codes.Internal, "failed to "+op)
	}
}

func clampPage(page, pageSize int32) (int, int) {
	p, size := int(page), int(pageSize)
	if p < 1 {
		p = 1
	}
	if size < 1 {
		size = 10
	}
	if size > 100 {
		size = 100
	}
	return p, size
}

func toProto(p *Payment) *pb.Payment {
	return &pb.Payment{
		Id:             p.ID,
		OrderId:        p.OrderID,
		UserId:         p.UserID,
		AmountCents:    p.AmountCents,
		Currency:       p.Currency,
		Status:         pb.PaymentStatus(p.Status),
		Method:         p.Method,
		ProviderRef:    p.ProviderRef,
		FailureReason:  p.FailureReason,
		IdempotencyKey: p.IdempotencyKey,
		CreatedAt:      timestamppb.New(p.CreatedAt),
		UpdatedAt:      timestamppb.New(p.UpdatedAt),
	}
}

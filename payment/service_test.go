package payment

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/payment/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/events"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type queuedEvent struct {
	topic string
	key   string
	event events.Payment
}

// fakeRepo is an in-memory Repository that enforces the same uniqueness rules as the real table.
type fakeRepo struct {
	mu       sync.Mutex
	payments map[string]*Payment
	outbox   []queuedEvent
	failNext error
}

func newFakeRepo() *fakeRepo { return &fakeRepo{payments: map[string]*Payment{}} }

func (f *fakeRepo) takeErr() error {
	err := f.failNext
	f.failNext = nil
	return err
}

func (f *fakeRepo) Insert(_ context.Context, p *Payment) (*Payment, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.takeErr(); err != nil {
		return nil, false, err
	}
	for _, existing := range f.payments {
		if existing.IdempotencyKey == p.IdempotencyKey || existing.OrderID == p.OrderID {
			cp := *existing
			return &cp, false, nil
		}
	}
	stored := *p
	stored.CreatedAt, stored.UpdatedAt = time.Now(), time.Now()
	f.payments[p.ID] = &stored
	cp := stored
	return &cp, true, nil
}

func (f *fakeRepo) GetByID(_ context.Context, id string) (*Payment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.takeErr(); err != nil {
		return nil, err
	}
	p, ok := f.payments[id]
	if !ok {
		return nil, ErrPaymentNotFound
	}
	cp := *p
	return &cp, nil
}

func (f *fakeRepo) GetByOrderID(_ context.Context, orderID string) (*Payment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.takeErr(); err != nil {
		return nil, err
	}
	for _, p := range f.payments {
		if p.OrderID == orderID {
			cp := *p
			return &cp, nil
		}
	}
	return nil, ErrPaymentNotFound
}

func (f *fakeRepo) ListByUser(_ context.Context, userID string, page, pageSize int) ([]*Payment, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*Payment
	for _, p := range f.payments {
		if p.UserID == userID {
			cp := *p
			out = append(out, &cp)
		}
	}
	return out, len(out), nil
}

func (f *fakeRepo) Apply(_ context.Context, t Transition) (*Payment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.takeErr(); err != nil {
		return nil, err
	}
	p, ok := f.payments[t.ID]
	if !ok || p.Status != t.From {
		return nil, ErrStatusChanged
	}
	p.Status = t.To
	if t.ProviderRef != "" {
		p.ProviderRef = t.ProviderRef
	}
	if t.FailureReason != "" {
		p.FailureReason = t.FailureReason
	}
	f.outbox = append(f.outbox, queuedEvent{topic: t.Topic, key: p.OrderID, event: t.Event.(events.Payment)})
	cp := *p
	return &cp, nil
}

type countingProcessor struct {
	MockProcessor
	charges   int
	refunds   int
	chargeErr error
	refundErr error
}

func (c *countingProcessor) Charge(ctx context.Context, req ChargeRequest) (string, error) {
	c.charges++
	if c.chargeErr != nil {
		return "", c.chargeErr
	}
	return c.MockProcessor.Charge(ctx, req)
}

func (c *countingProcessor) Refund(ctx context.Context, req RefundRequest) error {
	c.refunds++
	if c.refundErr != nil {
		return c.refundErr
	}
	return c.MockProcessor.Refund(ctx, req)
}

func newTestService() (*Service, *fakeRepo, *countingProcessor) {
	repo := newFakeRepo()
	proc := &countingProcessor{}
	return NewService(repo, proc, logger.New("payment-test")), repo, proc
}

func validInput() ProcessInput {
	return ProcessInput{OrderID: testOrderID, UserID: testUserID, AmountCents: 4999, Currency: "usd", Method: "card"}
}

func TestProcessChargesAndQueuesSucceededEvent(t *testing.T) {
	svc, repo, proc := newTestService()

	p, err := svc.Process(context.Background(), validInput())
	require.NoError(t, err)

	assert.Equal(t, StatusSucceeded, p.Status)
	assert.Equal(t, "USD", p.Currency)
	assert.Equal(t, testOrderID, p.IdempotencyKey, "key defaults to the order id")
	assert.Regexp(t, `^mock_`, p.ProviderRef)
	assert.Equal(t, 1, proc.charges)

	require.Len(t, repo.outbox, 1)
	assert.Equal(t, events.TopicPaymentSucceeded, repo.outbox[0].topic)
	assert.Equal(t, testOrderID, repo.outbox[0].key)
	assert.Equal(t, p.ID, repo.outbox[0].event.PaymentID)
	assert.NotEmpty(t, repo.outbox[0].event.EventID)
}

func TestProcessIsIdempotent(t *testing.T) {
	svc, repo, proc := newTestService()

	first, err := svc.Process(context.Background(), validInput())
	require.NoError(t, err)
	second, err := svc.Process(context.Background(), validInput())
	require.NoError(t, err)

	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, first.ProviderRef, second.ProviderRef)
	assert.Equal(t, 1, proc.charges, "retry must not charge again")
	assert.Len(t, repo.outbox, 1, "retry must not emit a second event")
}

func TestProcessRejectsKeyReusedForAnotherOrder(t *testing.T) {
	svc, _, _ := newTestService()
	in := validInput()
	in.IdempotencyKey = "checkout-1"
	_, err := svc.Process(context.Background(), in)
	require.NoError(t, err)

	in.OrderID = "44444444-4444-4444-4444-444444444444"
	_, err = svc.Process(context.Background(), in)
	assert.ErrorIs(t, err, ErrKeyReused)
}

func TestProcessDeclineIsFailedPaymentNotError(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*ProcessInput)
		reason string
	}{
		{"declined card", func(in *ProcessInput) { in.Method = "card_declined" }, "card declined"},
		{"over limit", func(in *ProcessInput) { in.AmountCents = 1_000_001 }, "amount exceeds limit"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, repo, _ := newTestService()
			in := validInput()
			tt.mutate(&in)

			resp, err := svc.ProcessPayment(context.Background(), &pb.ProcessPaymentRequest{
				OrderId: in.OrderID, UserId: in.UserID, AmountCents: in.AmountCents, Method: in.Method,
			})
			require.NoError(t, err)
			assert.Equal(t, pb.PaymentStatus_PAYMENT_STATUS_FAILED, resp.Payment.Status)
			assert.Equal(t, tt.reason, resp.Payment.FailureReason)

			require.Len(t, repo.outbox, 1)
			assert.Equal(t, events.TopicPaymentFailed, repo.outbox[0].topic)
			assert.Equal(t, tt.reason, repo.outbox[0].event.Reason)
		})
	}
}

func TestProcessResumesPendingPaymentAfterProviderError(t *testing.T) {
	svc, repo, proc := newTestService()
	proc.chargeErr = errors.New("provider timeout")

	_, err := svc.Process(context.Background(), validInput())
	require.Error(t, err)
	stuck, err := repo.GetByOrderID(context.Background(), testOrderID)
	require.NoError(t, err)
	assert.Equal(t, StatusPending, stuck.Status)

	proc.chargeErr = nil
	p, err := svc.Process(context.Background(), validInput())
	require.NoError(t, err)
	assert.Equal(t, stuck.ID, p.ID)
	assert.Equal(t, StatusSucceeded, p.Status)
	assert.Len(t, repo.outbox, 1)
}

func TestProcessPaymentValidation(t *testing.T) {
	tests := []struct {
		name string
		req  *pb.ProcessPaymentRequest
	}{
		{"missing order", &pb.ProcessPaymentRequest{UserId: testUserID, AmountCents: 1000}},
		{"bad order id", &pb.ProcessPaymentRequest{OrderId: "nope", UserId: testUserID, AmountCents: 1000}},
		{"missing user", &pb.ProcessPaymentRequest{OrderId: testOrderID, AmountCents: 1000}},
		{"zero amount", &pb.ProcessPaymentRequest{OrderId: testOrderID, UserId: testUserID}},
		{"negative amount", &pb.ProcessPaymentRequest{OrderId: testOrderID, UserId: testUserID, AmountCents: -100}},
		{"bad currency", &pb.ProcessPaymentRequest{OrderId: testOrderID, UserId: testUserID, AmountCents: 1000, Currency: "EURO"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, proc := newTestService()
			_, err := svc.ProcessPayment(context.Background(), tt.req)
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
			assert.Zero(t, proc.charges)
		})
	}
}

func TestProcessPaymentInternalErrorIsHidden(t *testing.T) {
	svc, repo, _ := newTestService()
	repo.failNext = errors.New("connection reset")

	_, err := svc.ProcessPayment(context.Background(), &pb.ProcessPaymentRequest{
		OrderId: testOrderID, UserId: testUserID, AmountCents: 1000,
	})
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.NotContains(t, err.Error(), "connection reset")
}

func TestRefundRules(t *testing.T) {
	ctx := context.Background()

	t.Run("succeeded payment is refunded", func(t *testing.T) {
		svc, repo, proc := newTestService()
		p, err := svc.Process(ctx, validInput())
		require.NoError(t, err)

		resp, err := svc.RefundPayment(ctx, &pb.RefundPaymentRequest{PaymentId: p.ID, Reason: "customer request"})
		require.NoError(t, err)
		assert.Equal(t, pb.PaymentStatus_PAYMENT_STATUS_REFUNDED, resp.Payment.Status)
		assert.Equal(t, 1, proc.refunds)
		require.Len(t, repo.outbox, 2)
		assert.Equal(t, events.TopicPaymentRefunded, repo.outbox[1].topic)
		assert.Equal(t, "customer request", repo.outbox[1].event.Reason)
	})

	t.Run("refunding twice fails", func(t *testing.T) {
		svc, _, proc := newTestService()
		p, err := svc.Process(ctx, validInput())
		require.NoError(t, err)
		_, err = svc.RefundPayment(ctx, &pb.RefundPaymentRequest{PaymentId: p.ID})
		require.NoError(t, err)

		_, err = svc.RefundPayment(ctx, &pb.RefundPaymentRequest{PaymentId: p.ID})
		assert.Equal(t, codes.FailedPrecondition, status.Code(err))
		assert.Equal(t, 1, proc.refunds)
	})

	t.Run("failed payment cannot be refunded", func(t *testing.T) {
		svc, _, proc := newTestService()
		in := validInput()
		in.Method = "card_declined"
		p, err := svc.Process(ctx, in)
		require.NoError(t, err)

		_, err = svc.RefundPayment(ctx, &pb.RefundPaymentRequest{PaymentId: p.ID})
		assert.Equal(t, codes.FailedPrecondition, status.Code(err))
		assert.Zero(t, proc.refunds)
	})

	t.Run("unknown payment", func(t *testing.T) {
		svc, _, _ := newTestService()
		_, err := svc.RefundPayment(ctx, &pb.RefundPaymentRequest{PaymentId: testPaymentID})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("provider refund error keeps payment succeeded", func(t *testing.T) {
		svc, repo, proc := newTestService()
		p, err := svc.Process(ctx, validInput())
		require.NoError(t, err)
		proc.refundErr = errors.New("provider down")

		_, err = svc.RefundPayment(ctx, &pb.RefundPaymentRequest{PaymentId: p.ID})
		assert.Equal(t, codes.Internal, status.Code(err))
		stored, _ := repo.GetByID(ctx, p.ID)
		assert.Equal(t, StatusSucceeded, stored.Status)
	})
}

func TestCancelBeforeCreateBlocksLateCharge(t *testing.T) {
	svc, repo, proc := newTestService()
	ctx := context.Background()

	require.NoError(t, svc.CancelOrder(ctx, testOrderID, testUserID, "changed my mind"))
	cancelled, err := repo.GetByOrderID(ctx, testOrderID)
	require.NoError(t, err)
	assert.Equal(t, StatusCancelled, cancelled.Status)

	p, err := svc.Process(ctx, validInput())
	require.NoError(t, err)
	assert.Equal(t, cancelled.ID, p.ID)
	assert.Equal(t, StatusCancelled, p.Status)
	assert.Zero(t, proc.charges)
	assert.Empty(t, repo.outbox)
}

func TestCancelAfterSuccessRefunds(t *testing.T) {
	svc, repo, proc := newTestService()
	ctx := context.Background()
	_, err := svc.Process(ctx, validInput())
	require.NoError(t, err)

	require.NoError(t, svc.CancelOrder(ctx, testOrderID, testUserID, "out of stock"))
	p, _ := repo.GetByOrderID(ctx, testOrderID)
	assert.Equal(t, StatusRefunded, p.Status)
	assert.Equal(t, 1, proc.refunds)

	// A redelivered cancellation is a no-op.
	require.NoError(t, svc.CancelOrder(ctx, testOrderID, testUserID, "out of stock"))
	assert.Equal(t, 1, proc.refunds)
	assert.Len(t, repo.outbox, 2)
}

func TestCancelAfterFailedPaymentIsNoop(t *testing.T) {
	svc, repo, proc := newTestService()
	ctx := context.Background()
	in := validInput()
	in.Method = "card_declined"
	_, err := svc.Process(ctx, in)
	require.NoError(t, err)

	require.NoError(t, svc.CancelOrder(ctx, testOrderID, testUserID, "payment failed"))
	p, _ := repo.GetByOrderID(ctx, testOrderID)
	assert.Equal(t, StatusFailed, p.Status)
	assert.Zero(t, proc.refunds)
}

func TestGetAndListPayments(t *testing.T) {
	svc, _, _ := newTestService()
	ctx := context.Background()
	p, err := svc.Process(ctx, validInput())
	require.NoError(t, err)

	got, err := svc.GetPayment(ctx, &pb.GetPaymentRequest{Id: p.ID})
	require.NoError(t, err)
	assert.Equal(t, p.ID, got.Payment.Id)

	byOrder, err := svc.GetPaymentByOrder(ctx, &pb.GetPaymentByOrderRequest{OrderId: testOrderID})
	require.NoError(t, err)
	assert.Equal(t, p.ID, byOrder.Payment.Id)

	_, err = svc.GetPayment(ctx, &pb.GetPaymentRequest{Id: testPaymentID})
	assert.Equal(t, codes.NotFound, status.Code(err))
	_, err = svc.GetPayment(ctx, &pb.GetPaymentRequest{})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	list, err := svc.ListPayments(ctx, &pb.ListPaymentsRequest{UserId: testUserID, Page: 0, PageSize: 500})
	require.NoError(t, err)
	assert.Equal(t, int32(1), list.Total)
	assert.Equal(t, int32(1), list.Page)
	assert.Equal(t, int32(100), list.PageSize)
	assert.Len(t, list.Payments, 1)

	_, err = svc.ListPayments(ctx, &pb.ListPaymentsRequest{})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

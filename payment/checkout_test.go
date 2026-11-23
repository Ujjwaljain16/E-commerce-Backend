package payment

import (
	"context"
	"encoding/json"
	"net/http"
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

func newRazorpayService(t *testing.T) (*Service, *fakeRepo, *fakeRazorpay) {
	t.Helper()
	fake := newFakeRazorpay(t)
	repo := newFakeRepo()
	svc := NewService(repo, &countingProcessor{}, logger.New("payment-test")).UseRazorpay(fake.client(t), 30*time.Minute)
	return svc, repo, fake
}

func inrInput() ProcessInput {
	return ProcessInput{OrderID: testOrderID, UserID: testUserID, AmountCents: 49900, Currency: "INR", Method: "razorpay"}
}

// openCheckout runs order.created through the service and returns the pending payment.
func openTestCheckout(t *testing.T, svc *Service) *Payment {
	t.Helper()
	p, err := svc.Process(context.Background(), inrInput())
	require.NoError(t, err)
	require.Equal(t, StatusPending, p.Status)
	require.NotEmpty(t, p.ProviderOrderID)
	return p
}

// webhook builds a signed Razorpay webhook in the documented envelope.
func webhook(t *testing.T, event string, p *razorpayPayment) ([]byte, string) {
	t.Helper()
	entity := map[string]interface{}{
		"id": p.ID, "entity": "payment", "amount": p.Amount, "currency": p.Currency, "status": p.Status,
		"order_id": p.OrderID, "method": "upi", "captured": p.Status == razorpayCaptured, "error_description": nil,
	}
	if p.ErrorDescription != "" {
		entity["error_description"] = p.ErrorDescription
	}
	body, err := json.Marshal(map[string]interface{}{
		"entity": "event", "account_id": "acc_test", "event": event, "contains": []string{"payment"},
		"payload": map[string]interface{}{"payment": map[string]interface{}{"entity": entity}}, "created_at": 1700000000,
	})
	require.NoError(t, err)
	return body, sign(testWebhookSecret, string(body))
}

func stored(t *testing.T, repo *fakeRepo) *Payment {
	t.Helper()
	p, err := repo.GetByOrderID(context.Background(), testOrderID)
	require.NoError(t, err)
	return p
}

func TestRazorpayOrderCreatedOpensCheckoutWithoutEvent(t *testing.T) {
	svc, repo, fake := newRazorpayService(t)

	p := openTestCheckout(t, svc)
	assert.Equal(t, ProviderRazorpay, p.Provider)
	assert.Equal(t, "order_test1", p.ProviderOrderID)
	assert.Equal(t, "INR", p.Currency)
	assert.Empty(t, repo.outbox, "the saga waits for the customer")

	// A redelivered order.created reuses the payment and the Razorpay order.
	require.NoError(t, svc.HandleMessage(context.Background(), message(t, events.TopicOrderCreated, events.OrderCreated{
		Meta: events.NewMeta(), OrderID: testOrderID, UserID: testUserID, TotalAmountCents: 49900, Currency: "INR", PaymentMethod: "razorpay",
	})))
	assert.Equal(t, 1, fake.createdCount())
	assert.Equal(t, p.ProviderOrderID, stored(t, repo).ProviderOrderID)
	assert.Empty(t, repo.outbox)
}

func TestRazorpayCheckoutReusesOrderCreatedBeforeACrash(t *testing.T) {
	svc, repo, fake := newRazorpayService(t)
	// The previous attempt stored the payment and created the Razorpay order, then died.
	_, _, err := repo.Insert(context.Background(), &Payment{
		ID: testPaymentID, OrderID: testOrderID, UserID: testUserID, AmountCents: 49900, Currency: "INR",
		Status: StatusPending, IdempotencyKey: testOrderID, Provider: ProviderRazorpay,
	})
	require.NoError(t, err)
	existing, err := fake.client(t).createOrder(context.Background(), testOrderID, 49900, "INR", nil)
	require.NoError(t, err)

	p, err := svc.Process(context.Background(), inrInput())
	require.NoError(t, err)
	assert.Equal(t, existing.ID, p.ProviderOrderID)
	assert.Equal(t, 1, fake.createdCount(), "no second Razorpay order")
}

func TestRazorpayRejectedOrderFailsPayment(t *testing.T) {
	svc, repo, _ := newRazorpayService(t)
	in := inrInput()
	in.AmountCents = 50 // below Razorpay's INR 1.00 minimum

	p, err := svc.Process(context.Background(), in)
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, p.Status)
	assert.Contains(t, p.FailureReason, "INR 1.00")
	require.Len(t, repo.outbox, 1)
	assert.Equal(t, events.TopicPaymentFailed, repo.outbox[0].topic)
}

func TestRazorpayOutageIsRetried(t *testing.T) {
	svc, repo, fake := newRazorpayService(t)
	fake.setFailure(http.StatusServiceUnavailable, "try later")

	_, err := svc.Process(context.Background(), inrInput())
	require.Error(t, err)
	assert.False(t, IsInvalidInput(err), "the consumer must retry, not skip")
	p := stored(t, repo)
	assert.Equal(t, StatusPending, p.Status)
	assert.Empty(t, p.ProviderOrderID)

	fake.setFailure(0, "")
	p = openTestCheckout(t, svc)
	assert.Equal(t, "order_test1", p.ProviderOrderID)
	assert.Empty(t, repo.outbox)
}

func TestConfirmPaymentSucceedsOnce(t *testing.T) {
	svc, repo, fake := newRazorpayService(t)
	p := openTestCheckout(t, svc)
	fake.pay(p.ProviderOrderID, "pay_1", razorpayCaptured)
	sig := sign(testKeySecret, p.ProviderOrderID+"|pay_1")

	resp, err := svc.ConfirmPayment(context.Background(), &pb.ConfirmPaymentRequest{OrderId: testOrderID, ProviderPaymentId: "pay_1", Signature: sig})
	require.NoError(t, err)
	assert.Equal(t, pb.PaymentStatus_PAYMENT_STATUS_SUCCEEDED, resp.Payment.Status)
	assert.Equal(t, "pay_1", resp.Payment.ProviderRef)
	assert.Equal(t, "razorpay", resp.Payment.Provider)

	// The client retrying, and the webhook arriving after, change nothing.
	_, err = svc.ConfirmPayment(context.Background(), &pb.ConfirmPaymentRequest{OrderId: testOrderID, ProviderPaymentId: "pay_1", Signature: sig})
	require.NoError(t, err)
	body, hookSig := webhook(t, "payment.captured", fake.pay(p.ProviderOrderID, "pay_1", razorpayCaptured))
	require.NoError(t, svc.HandleWebhook(context.Background(), body, hookSig))

	require.Len(t, repo.outbox, 1)
	assert.Equal(t, events.TopicPaymentSucceeded, repo.outbox[0].topic)
	assert.Equal(t, int64(49900), repo.outbox[0].event.AmountCents)
	assert.Equal(t, "INR", repo.outbox[0].event.Currency)
}

func TestConfirmPaymentCapturesAuthorizedPayment(t *testing.T) {
	svc, repo, fake := newRazorpayService(t)
	p := openTestCheckout(t, svc)
	fake.pay(p.ProviderOrderID, "pay_1", razorpayAuthorized)

	got, err := svc.Confirm(context.Background(), testOrderID, "pay_1", sign(testKeySecret, p.ProviderOrderID+"|pay_1"))
	require.NoError(t, err)
	assert.Equal(t, StatusSucceeded, got.Status)
	assert.Equal(t, []string{"pay_1"}, fake.captureCalls())
	assert.Len(t, repo.outbox, 1)
}

func TestConfirmPaymentGuards(t *testing.T) {
	ctx := context.Background()

	t.Run("bad signature changes nothing", func(t *testing.T) {
		svc, repo, fake := newRazorpayService(t)
		p := openTestCheckout(t, svc)
		fake.pay(p.ProviderOrderID, "pay_1", razorpayCaptured)

		for _, sig := range []string{"", "deadbeef", sign(testKeySecret, "order_someone_else|pay_1"), sign(testWebhookSecret, p.ProviderOrderID+"|pay_1")} {
			_, err := svc.ConfirmPayment(ctx, &pb.ConfirmPaymentRequest{OrderId: testOrderID, ProviderPaymentId: "pay_1", Signature: sig})
			assert.Equal(t, codes.InvalidArgument, status.Code(err))
		}
		assert.Equal(t, StatusPending, stored(t, repo).Status)
		assert.Empty(t, repo.outbox)
	})

	t.Run("payment of another order is rejected", func(t *testing.T) {
		svc, repo, fake := newRazorpayService(t)
		p := openTestCheckout(t, svc)
		// A genuine payment, but for a different Razorpay order with the same amount.
		other, err := fake.client(t).createOrder(ctx, "other-receipt", 49900, "INR", nil)
		require.NoError(t, err)
		fake.pay(other.ID, "pay_other", razorpayCaptured)

		_, err = svc.ConfirmPayment(ctx, &pb.ConfirmPaymentRequest{
			OrderId: testOrderID, ProviderPaymentId: "pay_other", Signature: sign(testKeySecret, p.ProviderOrderID+"|pay_other"),
		})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		assert.Equal(t, StatusPending, stored(t, repo).Status)
	})

	t.Run("missing input", func(t *testing.T) {
		svc, _, _ := newRazorpayService(t)
		_, err := svc.ConfirmPayment(ctx, &pb.ConfirmPaymentRequest{OrderId: "nope", ProviderPaymentId: "pay_1", Signature: "x"})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
		_, err = svc.ConfirmPayment(ctx, &pb.ConfirmPaymentRequest{OrderId: testOrderID, Signature: "x"})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	})

	t.Run("unknown order", func(t *testing.T) {
		svc, _, _ := newRazorpayService(t)
		_, err := svc.ConfirmPayment(ctx, &pb.ConfirmPaymentRequest{OrderId: testOrderID, ProviderPaymentId: "pay_1", Signature: "x"})
		assert.Equal(t, codes.NotFound, status.Code(err))
	})

	t.Run("mock payment", func(t *testing.T) {
		svc, repo, _ := newRazorpayService(t)
		_, _, err := repo.Insert(ctx, &Payment{ID: testPaymentID, OrderID: testOrderID, UserID: testUserID, Status: StatusSucceeded, IdempotencyKey: testOrderID, Provider: ProviderMock})
		require.NoError(t, err)
		_, err = svc.ConfirmPayment(ctx, &pb.ConfirmPaymentRequest{OrderId: testOrderID, ProviderPaymentId: "pay_1", Signature: "x"})
		assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	})

	t.Run("mock provider", func(t *testing.T) {
		svc, _, _ := newTestService()
		_, err := svc.ConfirmPayment(ctx, &pb.ConfirmPaymentRequest{OrderId: testOrderID, ProviderPaymentId: "pay_1", Signature: "x"})
		assert.Equal(t, codes.FailedPrecondition, status.Code(err))
		_, err = svc.HandleProviderWebhook(ctx, &pb.HandleProviderWebhookRequest{Body: []byte(`{}`), Signature: "x"})
		assert.Equal(t, codes.FailedPrecondition, status.Code(err))
	})

	t.Run("razorpay down", func(t *testing.T) {
		svc, repo, fake := newRazorpayService(t)
		p := openTestCheckout(t, svc)
		fake.setFailure(http.StatusInternalServerError, "")
		_, err := svc.ConfirmPayment(ctx, &pb.ConfirmPaymentRequest{OrderId: testOrderID, ProviderPaymentId: "pay_1", Signature: sign(testKeySecret, p.ProviderOrderID+"|pay_1")})
		assert.Equal(t, codes.Unavailable, status.Code(err))
		assert.Equal(t, StatusPending, stored(t, repo).Status)
	})
}

func TestWebhookCapturedIsIdempotent(t *testing.T) {
	svc, repo, fake := newRazorpayService(t)
	p := openTestCheckout(t, svc)
	body, sig := webhook(t, "payment.captured", fake.pay(p.ProviderOrderID, "pay_1", razorpayCaptured))

	for i := 0; i < 3; i++ {
		_, err := svc.HandleProviderWebhook(context.Background(), &pb.HandleProviderWebhookRequest{Body: body, Signature: sig})
		require.NoError(t, err)
	}
	got := stored(t, repo)
	assert.Equal(t, StatusSucceeded, got.Status)
	assert.Equal(t, "pay_1", got.ProviderRef)
	require.Len(t, repo.outbox, 1)
	assert.Equal(t, events.TopicPaymentSucceeded, repo.outbox[0].topic)
}

func TestWebhookAuthorizedIsCaptured(t *testing.T) {
	svc, repo, fake := newRazorpayService(t)
	p := openTestCheckout(t, svc)
	body, sig := webhook(t, "payment.authorized", fake.pay(p.ProviderOrderID, "pay_1", razorpayAuthorized))

	require.NoError(t, svc.HandleWebhook(context.Background(), body, sig))
	assert.Equal(t, StatusSucceeded, stored(t, repo).Status)
	assert.Equal(t, []string{"pay_1"}, fake.captureCalls())

	// The authorized event replayed after capture neither captures again nor emits again.
	require.NoError(t, svc.HandleWebhook(context.Background(), body, sig))
	assert.Len(t, fake.captureCalls(), 1)
	assert.Len(t, repo.outbox, 1)
}

func TestWebhookFailedKeepsProviderReason(t *testing.T) {
	svc, repo, fake := newRazorpayService(t)
	p := openTestCheckout(t, svc)
	body, sig := webhook(t, "payment.failed", fake.pay(p.ProviderOrderID, "pay_1", razorpayFailed))

	require.NoError(t, svc.HandleWebhook(context.Background(), body, sig))
	require.NoError(t, svc.HandleWebhook(context.Background(), body, sig))

	got := stored(t, repo)
	assert.Equal(t, StatusFailed, got.Status)
	assert.Equal(t, "Payment was declined by the bank", got.FailureReason)
	require.Len(t, repo.outbox, 1)
	assert.Equal(t, events.TopicPaymentFailed, repo.outbox[0].topic)
	assert.Equal(t, "Payment was declined by the bank", repo.outbox[0].event.Reason)
}

func TestWebhookArrivingOutOfOrder(t *testing.T) {
	svc, repo, fake := newRazorpayService(t)
	p := openTestCheckout(t, svc)
	captured, capturedSig := webhook(t, "payment.captured", fake.pay(p.ProviderOrderID, "pay_1", razorpayCaptured))
	require.NoError(t, svc.HandleWebhook(context.Background(), captured, capturedSig))

	// An earlier failed attempt reported late must not undo the success.
	failed, failedSig := webhook(t, "payment.failed", fake.pay(p.ProviderOrderID, "pay_0", razorpayFailed))
	require.NoError(t, svc.HandleWebhook(context.Background(), failed, failedSig))

	assert.Equal(t, StatusSucceeded, stored(t, repo).Status)
	assert.Len(t, repo.outbox, 1)
	assert.Empty(t, fake.refundCalls())
}

func TestWebhookBadSignatureChangesNothing(t *testing.T) {
	svc, repo, fake := newRazorpayService(t)
	p := openTestCheckout(t, svc)
	body, sig := webhook(t, "payment.captured", fake.pay(p.ProviderOrderID, "pay_1", razorpayCaptured))

	for _, bad := range []string{"", sig[:len(sig)-1] + "0", sign(testKeySecret, string(body))} {
		_, err := svc.HandleProviderWebhook(context.Background(), &pb.HandleProviderWebhookRequest{Body: body, Signature: bad})
		assert.Equal(t, codes.InvalidArgument, status.Code(err))
	}
	tampered := append([]byte{}, body...)
	tampered[len(tampered)-2] = '1'
	_, err := svc.HandleProviderWebhook(context.Background(), &pb.HandleProviderWebhookRequest{Body: tampered, Signature: sig})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))

	assert.Equal(t, StatusPending, stored(t, repo).Status)
	assert.Empty(t, repo.outbox)
}

func TestWebhookIgnoresEventsThatAreNotOurs(t *testing.T) {
	svc, repo, fake := newRazorpayService(t)
	p := openTestCheckout(t, svc)

	refund := []byte(`{"entity":"event","event":"refund.processed","payload":{"refund":{"entity":{"id":"rfnd_1"}}}}`)
	require.NoError(t, svc.HandleWebhook(context.Background(), refund, sign(testWebhookSecret, string(refund))))

	unknown, sig := webhook(t, "payment.captured", &razorpayPayment{ID: "pay_x", OrderID: "order_elsewhere", Amount: 100, Currency: "INR", Status: razorpayCaptured})
	require.NoError(t, svc.HandleWebhook(context.Background(), unknown, sig))

	wrongAmount := fake.pay(p.ProviderOrderID, "pay_1", razorpayCaptured)
	wrongAmount.Amount = 1
	body, sig := webhook(t, "payment.captured", wrongAmount)
	require.NoError(t, svc.HandleWebhook(context.Background(), body, sig), "acknowledged so Razorpay stops retrying, but not applied")

	notJSON := []byte(`not json`)
	require.NoError(t, svc.HandleWebhook(context.Background(), notJSON, sign(testWebhookSecret, string(notJSON))))

	assert.Equal(t, StatusPending, stored(t, repo).Status)
	assert.Empty(t, repo.outbox)
}

func TestExpirePendingFailsUnpaidCheckout(t *testing.T) {
	svc, repo, _ := newRazorpayService(t)
	openTestCheckout(t, svc)

	n, err := svc.ExpirePending(context.Background())
	require.NoError(t, err)
	assert.Zero(t, n, "not expired before the timeout")

	svc.now = func() time.Time { return time.Now().Add(31 * time.Minute) }
	n, err = svc.ExpirePending(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 1, n)
	got := stored(t, repo)
	assert.Equal(t, StatusFailed, got.Status)
	assert.Equal(t, "payment not completed in time", got.FailureReason)
	require.Len(t, repo.outbox, 1)
	assert.Equal(t, events.TopicPaymentFailed, repo.outbox[0].topic)

	n, err = svc.ExpirePending(context.Background())
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestExpirePendingCompletesPaymentWhoseWebhookWasMissed(t *testing.T) {
	svc, repo, fake := newRazorpayService(t)
	p := openTestCheckout(t, svc)
	fake.pay(p.ProviderOrderID, "pay_0", razorpayFailed)
	fake.pay(p.ProviderOrderID, "pay_1", razorpayCaptured)

	svc.now = func() time.Time { return time.Now().Add(time.Hour) }
	_, err := svc.ExpirePending(context.Background())
	require.NoError(t, err)

	got := stored(t, repo)
	assert.Equal(t, StatusSucceeded, got.Status)
	assert.Equal(t, "pay_1", got.ProviderRef)
	require.Len(t, repo.outbox, 1)
	assert.Equal(t, events.TopicPaymentSucceeded, repo.outbox[0].topic)
}

func TestExpirePendingWaitsWhileRazorpayIsDown(t *testing.T) {
	svc, repo, fake := newRazorpayService(t)
	openTestCheckout(t, svc)
	fake.setFailure(http.StatusBadGateway, "")

	svc.now = func() time.Time { return time.Now().Add(time.Hour) }
	n, err := svc.ExpirePending(context.Background())
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Equal(t, StatusPending, stored(t, repo).Status, "money may have been captured, so do not fail blindly")
}

func TestExpirePendingIsSafeAcrossReplicas(t *testing.T) {
	fake := newFakeRazorpay(t)
	repo := newFakeRepo()
	later := func() time.Time { return time.Now().Add(time.Hour) }
	replicas := make([]*Service, 4)
	for i := range replicas {
		replicas[i] = NewService(repo, &countingProcessor{}, logger.New("payment-test")).UseRazorpay(fake.client(t), 30*time.Minute)
		replicas[i].now = later
	}
	openTestCheckout(t, replicas[0])

	var wg sync.WaitGroup
	for _, svc := range replicas {
		wg.Add(1)
		go func(svc *Service) {
			defer wg.Done()
			_, _ = svc.ExpirePending(context.Background())
		}(svc)
	}
	wg.Wait()

	assert.Equal(t, StatusFailed, stored(t, repo).Status)
	assert.Len(t, repo.outbox, 1, "exactly one payment.failed")
}

func TestCancelPendingCheckoutThenLateCaptureIsRefunded(t *testing.T) {
	svc, repo, fake := newRazorpayService(t)
	p := openTestCheckout(t, svc)

	require.NoError(t, svc.CancelOrder(context.Background(), testOrderID, testUserID, "changed my mind"))
	assert.Equal(t, StatusCancelled, stored(t, repo).Status)
	assert.Empty(t, repo.outbox, "cancelling a checkout publishes nothing")

	// The customer still had Checkout open and paid.
	body, sig := webhook(t, "payment.captured", fake.pay(p.ProviderOrderID, "pay_1", razorpayCaptured))
	require.NoError(t, svc.HandleWebhook(context.Background(), body, sig))
	require.NoError(t, svc.HandleWebhook(context.Background(), body, sig))

	got := stored(t, repo)
	assert.Equal(t, StatusRefunded, got.Status)
	assert.Equal(t, "pay_1", got.ProviderRef)
	assert.Equal(t, []refundCall{{paymentID: "pay_1", amount: 49900, key: "refund_pay_1"}}, fake.refundCalls())
	assert.Empty(t, repo.outbox, "the order is already cancelled")
}

func TestLateConfirmAfterExpiryIsRefunded(t *testing.T) {
	svc, repo, fake := newRazorpayService(t)
	p := openTestCheckout(t, svc)
	svc.now = func() time.Time { return time.Now().Add(time.Hour) }
	_, err := svc.ExpirePending(context.Background())
	require.NoError(t, err)

	fake.pay(p.ProviderOrderID, "pay_1", razorpayCaptured)
	got, err := svc.Confirm(context.Background(), testOrderID, "pay_1", sign(testKeySecret, p.ProviderOrderID+"|pay_1"))
	require.NoError(t, err)
	assert.Equal(t, StatusRefunded, got.Status)
	assert.Len(t, fake.refundCalls(), 1)
	assert.Len(t, repo.outbox, 1, "only the expiry's payment.failed")
}

func TestLateAuthorizationAfterCancelIsNotCaptured(t *testing.T) {
	svc, repo, fake := newRazorpayService(t)
	p := openTestCheckout(t, svc)
	require.NoError(t, svc.CancelOrder(context.Background(), testOrderID, testUserID, ""))

	body, sig := webhook(t, "payment.authorized", fake.pay(p.ProviderOrderID, "pay_1", razorpayAuthorized))
	require.NoError(t, svc.HandleWebhook(context.Background(), body, sig))
	assert.Empty(t, fake.captureCalls(), "Razorpay releases uncaptured payments itself")
	assert.Equal(t, StatusCancelled, stored(t, repo).Status)
}

func TestCancelAfterRazorpaySuccessRefundsThroughRazorpay(t *testing.T) {
	svc, repo, fake := newRazorpayService(t)
	p := openTestCheckout(t, svc)
	fake.pay(p.ProviderOrderID, "pay_1", razorpayCaptured)
	_, err := svc.Confirm(context.Background(), testOrderID, "pay_1", sign(testKeySecret, p.ProviderOrderID+"|pay_1"))
	require.NoError(t, err)

	require.NoError(t, svc.CancelOrder(context.Background(), testOrderID, testUserID, "out of stock"))
	require.NoError(t, svc.CancelOrder(context.Background(), testOrderID, testUserID, "out of stock"))

	assert.Equal(t, StatusRefunded, stored(t, repo).Status)
	assert.Equal(t, []refundCall{{paymentID: "pay_1", amount: 49900, key: "refund_pay_1"}}, fake.refundCalls())
	require.Len(t, repo.outbox, 2)
	assert.Equal(t, events.TopicPaymentRefunded, repo.outbox[1].topic)
}

func TestMockProviderIsUnchanged(t *testing.T) {
	svc, repo, _ := newTestService()
	p, err := svc.Process(context.Background(), validInput())
	require.NoError(t, err)
	assert.Equal(t, ProviderMock, p.Provider)
	assert.Empty(t, p.ProviderOrderID)
	assert.Equal(t, StatusSucceeded, p.Status)
	assert.Len(t, repo.outbox, 1)

	n, err := svc.ExpirePending(context.Background())
	require.NoError(t, err)
	assert.Zero(t, n)
}

func TestLoadProviderConfig(t *testing.T) {
	env := func(kv map[string]string) func(string) string {
		return func(k string) string { return kv[k] }
	}

	cfg, err := LoadProviderConfig(env(nil))
	require.NoError(t, err)
	assert.Equal(t, ProviderMock, cfg.Provider)
	assert.Equal(t, 30*time.Minute, cfg.Timeout)

	full := map[string]string{
		"PAYMENT_PROVIDER": "razorpay", "PAYMENT_TIMEOUT": "15m",
		"RAZORPAY_KEY_ID": "rzp_test_x", "RAZORPAY_KEY_SECRET": "s", "RAZORPAY_WEBHOOK_SECRET": "w",
	}
	cfg, err = LoadProviderConfig(env(full))
	require.NoError(t, err)
	assert.Equal(t, ProviderRazorpay, cfg.Provider)
	assert.Equal(t, 15*time.Minute, cfg.Timeout)
	assert.Equal(t, "rzp_test_x", cfg.Razorpay.KeyID)

	for _, missing := range []string{"RAZORPAY_KEY_ID", "RAZORPAY_KEY_SECRET", "RAZORPAY_WEBHOOK_SECRET"} {
		kv := map[string]string{}
		for k, v := range full {
			kv[k] = v
		}
		delete(kv, missing)
		_, err := LoadProviderConfig(env(kv))
		require.Error(t, err, missing)
		assert.Contains(t, err.Error(), missing)
	}

	_, err = LoadProviderConfig(env(map[string]string{"PAYMENT_PROVIDER": "stripe"}))
	assert.Error(t, err)
	_, err = LoadProviderConfig(env(map[string]string{"PAYMENT_TIMEOUT": "soon"}))
	assert.Error(t, err)
}

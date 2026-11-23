package payment

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/events"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/kafka"
	kafkago "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func message(t *testing.T, topic string, v interface{}) kafkago.Message {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return kafkago.Message{Topic: topic, Key: []byte(testOrderID), Value: b}
}

func orderCreated() events.OrderCreated {
	return events.OrderCreated{
		Meta: events.NewMeta(), OrderID: testOrderID, UserID: testUserID,
		TotalAmount: 120.5, Currency: "USD", PaymentMethod: "card",
	}
}

func TestHandleOrderCreatedChargesOnce(t *testing.T) {
	svc, repo, proc := newTestService()
	msg := message(t, events.TopicOrderCreated, orderCreated())

	require.NoError(t, svc.HandleMessage(context.Background(), msg))
	require.NoError(t, svc.HandleMessage(context.Background(), msg))

	p, err := repo.GetByOrderID(context.Background(), testOrderID)
	require.NoError(t, err)
	assert.Equal(t, StatusSucceeded, p.Status)
	assert.Equal(t, 120.5, p.Amount)
	assert.Equal(t, "card", p.Method)
	assert.Equal(t, 1, proc.charges)
}

func TestHandleOrderCancelledThenLateCreated(t *testing.T) {
	svc, repo, proc := newTestService()
	ctx := context.Background()

	cancelled := events.OrderCancelled{Meta: events.NewMeta(), OrderID: testOrderID, UserID: testUserID, Reason: "user cancelled"}
	require.NoError(t, svc.HandleMessage(ctx, message(t, events.TopicOrderCancelled, cancelled)))
	require.NoError(t, svc.HandleMessage(ctx, message(t, events.TopicOrderCreated, orderCreated())))

	p, err := repo.GetByOrderID(ctx, testOrderID)
	require.NoError(t, err)
	assert.Equal(t, StatusCancelled, p.Status)
	assert.Equal(t, "user cancelled", p.FailureReason)
	assert.Zero(t, proc.charges)
}

func TestHandleMessageSkipsPoisonMessages(t *testing.T) {
	bad := orderCreated()
	bad.TotalAmount = 0
	noUser := events.OrderCancelled{OrderID: testOrderID}

	tests := []struct {
		name string
		msg  kafkago.Message
	}{
		{"malformed created", kafkago.Message{Topic: events.TopicOrderCreated, Value: []byte("{not json")}},
		{"malformed cancelled", kafkago.Message{Topic: events.TopicOrderCancelled, Value: []byte("[]")}},
		{"zero amount", message(t, events.TopicOrderCreated, bad)},
		{"cancel without user", message(t, events.TopicOrderCancelled, noUser)},
		{"unknown topic", kafkago.Message{Topic: "order.shipped", Value: []byte("{}")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _ := newTestService()
			err := svc.HandleMessage(context.Background(), tt.msg)
			assert.ErrorIs(t, err, kafka.ErrSkip)
		})
	}
}

func TestHandleMessageReturnsTransientErrorsForRetry(t *testing.T) {
	svc, repo, _ := newTestService()
	repo.failNext = errors.New("database unavailable")

	err := svc.HandleMessage(context.Background(), message(t, events.TopicOrderCreated, orderCreated()))
	require.Error(t, err)
	assert.NotErrorIs(t, err, kafka.ErrSkip)
}

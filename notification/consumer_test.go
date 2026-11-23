package notification

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	accountpb "github.com/Ujjwaljain16/E-commerce-Backend/account/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/notification/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/events"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/kafka"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	kafkago "github.com/segmentio/kafka-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeRepo keeps one row per event, like the unique event_id column.
type fakeRepo struct {
	mu      sync.Mutex
	byEvent map[string]*Notification
	saveErr error
}

func newFakeRepo() *fakeRepo { return &fakeRepo{byEvent: map[string]*Notification{}} }

func (f *fakeRepo) WasSent(_ context.Context, eventID string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.byEvent[eventID]
	return ok && n.Status == StatusSent, nil
}

func (f *fakeRepo) Save(_ context.Context, n *Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.saveErr != nil {
		return f.saveErr
	}
	if existing, ok := f.byEvent[n.EventID]; ok && existing.Status == StatusSent {
		return nil
	}
	cp := *n
	f.byEvent[n.EventID] = &cp
	return nil
}

func (f *fakeRepo) ListByUser(_ context.Context, userID string, _, _ int) ([]*Notification, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*Notification
	for _, n := range f.byEvent {
		if n.UserID == userID {
			cp := *n
			out = append(out, &cp)
		}
	}
	return out, len(out), nil
}

type fakeAccounts struct {
	user *accountpb.User
	err  error
}

func (f *fakeAccounts) GetProfile(_ context.Context, in *accountpb.GetProfileRequest, _ ...grpc.CallOption) (*accountpb.GetProfileResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &accountpb.GetProfileResponse{User: f.user}, nil
}

type fakeSender struct {
	sent []Email
	err  error
}

func (f *fakeSender) Send(_ context.Context, e Email) error {
	if f.err != nil {
		return f.err
	}
	f.sent = append(f.sent, e)
	return nil
}

func newTestService() (*Service, *fakeRepo, *fakeAccounts, *fakeSender) {
	repo := newFakeRepo()
	accounts := &fakeAccounts{user: &accountpb.User{Id: testUserID, Email: "asha@example.com", Name: "Asha"}}
	sender := &fakeSender{}
	return NewService(repo, accounts, sender, logger.New("notification-test")), repo, accounts, sender
}

func message(t *testing.T, topic string, v interface{}) kafkago.Message {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return kafkago.Message{Topic: topic, Value: b}
}

func createdMessage(t *testing.T) (kafkago.Message, string) {
	e := events.OrderCreated{
		Meta: events.NewMeta(), OrderID: testOrderID, UserID: testUserID, TotalAmountCents: 5998, Currency: "USD",
		Items: []events.OrderItem{{ProductName: "Mug", Quantity: 2, PriceCents: 2999}},
	}
	return message(t, events.TopicOrderCreated, e), e.EventID
}

func TestHandleOrderEventsSendEmail(t *testing.T) {
	tests := []struct {
		topic   string
		event   interface{}
		subject string
		body    string
	}{
		{events.TopicOrderCreated, events.OrderCreated{Meta: events.NewMeta(), OrderID: testOrderID, UserID: testUserID, TotalAmountCents: 5998, Currency: "USD"},
			"We received your order " + testOrderID, "$59.98"},
		{events.TopicOrderConfirmed, events.OrderConfirmed{Meta: events.NewMeta(), OrderID: testOrderID, UserID: testUserID, TotalAmountCents: 5998},
			"Your order " + testOrderID + " is confirmed", "($59.98)"},
		{events.TopicOrderCancelled, events.OrderCancelled{Meta: events.NewMeta(), OrderID: testOrderID, UserID: testUserID, Reason: "payment declined"},
			"Your order " + testOrderID + " was cancelled", "(payment declined)"},
	}
	for _, tt := range tests {
		t.Run(tt.topic, func(t *testing.T) {
			svc, repo, _, sender := newTestService()

			require.NoError(t, svc.HandleMessage(context.Background(), message(t, tt.topic, tt.event)))

			require.Len(t, sender.sent, 1)
			assert.Equal(t, "asha@example.com", sender.sent[0].To)
			assert.Equal(t, tt.subject, sender.sent[0].Subject)
			assert.Contains(t, sender.sent[0].Body, "Hi Asha,")
			assert.Contains(t, sender.sent[0].Body, tt.body)

			require.Len(t, repo.byEvent, 1)
			for _, n := range repo.byEvent {
				assert.Equal(t, StatusSent, n.Status)
				assert.Equal(t, tt.topic, n.Type)
				assert.Equal(t, ChannelEmail, n.Channel)
			}
		})
	}
}

func TestHandleMessageDedupesByEventID(t *testing.T) {
	svc, repo, _, sender := newTestService()
	msg, _ := createdMessage(t)

	require.NoError(t, svc.HandleMessage(context.Background(), msg))
	require.NoError(t, svc.HandleMessage(context.Background(), msg))

	assert.Len(t, sender.sent, 1)
	assert.Len(t, repo.byEvent, 1)
}

func TestHandleMessageSendFailureIsRetriedWithoutDuplicates(t *testing.T) {
	svc, repo, _, sender := newTestService()
	msg, eventID := createdMessage(t)

	sender.err = errors.New("smtp: 421 try again later")
	err := svc.HandleMessage(context.Background(), msg)
	require.Error(t, err)
	assert.NotErrorIs(t, err, kafka.ErrSkip)
	require.Contains(t, repo.byEvent, eventID)
	assert.Equal(t, StatusFailed, repo.byEvent[eventID].Status)
	assert.Contains(t, repo.byEvent[eventID].Error, "421")

	sender.err = nil
	require.NoError(t, svc.HandleMessage(context.Background(), msg))
	assert.Len(t, repo.byEvent, 1)
	assert.Equal(t, StatusSent, repo.byEvent[eventID].Status)
	assert.Len(t, sender.sent, 1)
}

func TestHandleMessageAccountLookup(t *testing.T) {
	t.Run("unknown user is skipped", func(t *testing.T) {
		svc, repo, accounts, sender := newTestService()
		accounts.err = status.Error(codes.NotFound, "account not found")
		msg, _ := createdMessage(t)

		assert.ErrorIs(t, svc.HandleMessage(context.Background(), msg), kafka.ErrSkip)
		assert.Empty(t, sender.sent)
		assert.Empty(t, repo.byEvent)
	})

	t.Run("account service outage is retried", func(t *testing.T) {
		svc, _, accounts, sender := newTestService()
		accounts.err = status.Error(codes.Unavailable, "connection refused")
		msg, _ := createdMessage(t)

		err := svc.HandleMessage(context.Background(), msg)
		require.Error(t, err)
		assert.NotErrorIs(t, err, kafka.ErrSkip)
		assert.Empty(t, sender.sent)
	})

	t.Run("user without email is skipped", func(t *testing.T) {
		svc, _, accounts, _ := newTestService()
		accounts.user = &accountpb.User{Id: testUserID}
		msg, _ := createdMessage(t)

		assert.ErrorIs(t, svc.HandleMessage(context.Background(), msg), kafka.ErrSkip)
	})
}

func TestHandleMessageSkipsPoisonMessages(t *testing.T) {
	tests := []struct {
		name string
		msg  kafkago.Message
	}{
		{"malformed json", kafkago.Message{Topic: events.TopicOrderCreated, Value: []byte("{oops")}},
		{"wrong shape", kafkago.Message{Topic: events.TopicOrderConfirmed, Value: []byte(`"just a string"`)}},
		{"missing event id", message(t, events.TopicOrderCancelled, events.OrderCancelled{OrderID: testOrderID, UserID: testUserID})},
		{"invalid user id", message(t, events.TopicOrderCancelled, events.OrderCancelled{Meta: events.NewMeta(), OrderID: testOrderID, UserID: "bob"})},
		{"missing order id", message(t, events.TopicOrderConfirmed, events.OrderConfirmed{Meta: events.NewMeta(), UserID: testUserID})},
		{"unknown topic", kafkago.Message{Topic: "payment.succeeded", Value: []byte("{}")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, _, _, sender := newTestService()
			assert.ErrorIs(t, svc.HandleMessage(context.Background(), tt.msg), kafka.ErrSkip)
			assert.Empty(t, sender.sent)
		})
	}
}

func TestListNotifications(t *testing.T) {
	svc, _, _, _ := newTestService()
	msg, _ := createdMessage(t)
	require.NoError(t, svc.HandleMessage(context.Background(), msg))

	resp, err := svc.ListNotifications(context.Background(), &pb.ListNotificationsRequest{UserId: testUserID, PageSize: 1000})
	require.NoError(t, err)
	assert.Equal(t, int32(1), resp.Total)
	assert.Equal(t, int32(1), resp.Page)
	assert.Equal(t, int32(100), resp.PageSize)
	require.Len(t, resp.Notifications, 1)
	assert.Equal(t, StatusSent, resp.Notifications[0].Status)
	assert.Equal(t, "asha@example.com", resp.Notifications[0].Recipient)

	_, err = svc.ListNotifications(context.Background(), &pb.ListNotificationsRequest{})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	_, err = svc.ListNotifications(context.Background(), &pb.ListNotificationsRequest{UserId: "nope"})
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

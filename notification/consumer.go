package notification

import (
	"context"
	"encoding/json"
	"fmt"

	accountpb "github.com/Ujjwaljain16/E-commerce-Backend/account/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/events"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/kafka"
	"github.com/google/uuid"
	kafkago "github.com/segmentio/kafka-go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ConsumedTopics are the order events that trigger a customer email.
var ConsumedTopics = []string{events.TopicOrderCreated, events.TopicOrderConfirmed, events.TopicOrderCancelled}

// orderEvent is the common shape of the order events after decoding.
type orderEvent struct {
	eventID string
	userID  string
	data    messageData
}

func decode(msg kafkago.Message) (orderEvent, error) {
	var ev orderEvent
	var err error
	switch msg.Topic {
	case events.TopicOrderCreated:
		var e events.OrderCreated
		err = json.Unmarshal(msg.Value, &e)
		ev = orderEvent{e.EventID, e.UserID, messageData{
			OrderID: e.OrderID, Total: formatTotal(e.TotalAmount, e.Currency), Items: e.Items,
		}}
	case events.TopicOrderConfirmed:
		var e events.OrderConfirmed
		err = json.Unmarshal(msg.Value, &e)
		ev = orderEvent{e.EventID, e.UserID, messageData{OrderID: e.OrderID, Total: formatTotal(e.TotalAmount, "")}}
	case events.TopicOrderCancelled:
		var e events.OrderCancelled
		err = json.Unmarshal(msg.Value, &e)
		ev = orderEvent{e.EventID, e.UserID, messageData{OrderID: e.OrderID, Reason: e.Reason}}
	default:
		return ev, fmt.Errorf("unexpected topic %s", msg.Topic)
	}
	if err != nil {
		return ev, fmt.Errorf("decode %s: %v", msg.Topic, err)
	}

	switch {
	case ev.eventID == "" || len(ev.eventID) > 64:
		return ev, fmt.Errorf("%s has a missing or oversized event_id", msg.Topic)
	case ev.data.OrderID == "":
		return ev, fmt.Errorf("%s has no order_id", msg.Topic)
	}
	if _, err := uuid.Parse(ev.userID); err != nil {
		return ev, fmt.Errorf("%s has an invalid user_id %q", msg.Topic, ev.userID)
	}
	return ev, nil
}

// HandleMessage is the kafka.Handler for order events. Each event produces at most one SENT
// notification; failed deliveries are recorded and returned as errors so the consumer retries them.
func (s *Service) HandleMessage(ctx context.Context, msg kafkago.Message) error {
	ev, err := decode(msg)
	if err != nil {
		return fmt.Errorf("%w: %v", kafka.ErrSkip, err)
	}

	sent, err := s.repo.WasSent(ctx, ev.eventID)
	if err != nil {
		return err
	}
	if sent {
		s.log.Debug(ctx, "Notification already sent", map[string]interface{}{"event_id": ev.eventID})
		return nil
	}

	lookupCtx, cancel := context.WithTimeout(ctx, s.lookupTimeout)
	profile, err := s.accounts.GetProfile(lookupCtx, &accountpb.GetProfileRequest{UserId: ev.userID})
	cancel()
	if err != nil {
		if code := status.Code(err); code == codes.NotFound || code == codes.InvalidArgument {
			return fmt.Errorf("%w: no account for user %s", kafka.ErrSkip, ev.userID)
		}
		return fmt.Errorf("look up user %s: %w", ev.userID, err)
	}
	user := profile.GetUser()
	if user.GetEmail() == "" {
		return fmt.Errorf("%w: user %s has no email", kafka.ErrSkip, ev.userID)
	}

	ev.data.Name = user.GetName()
	subject, body, err := render(msg.Topic, ev.data)
	if err != nil {
		return err
	}

	n := &Notification{
		ID:        uuid.NewString(),
		UserID:    ev.userID,
		EventID:   ev.eventID,
		Type:      msg.Topic,
		Channel:   ChannelEmail,
		Recipient: user.GetEmail(),
		Subject:   subject,
		Body:      body,
		Status:    StatusSent,
	}
	sendErr := s.sender.Send(ctx, Email{To: n.Recipient, Subject: subject, Body: body})
	if sendErr != nil {
		n.Status, n.Error = StatusFailed, sendErr.Error()
	}
	if err := s.repo.Save(ctx, n); err != nil {
		return err
	}
	if sendErr != nil {
		return fmt.Errorf("send %s notification: %w", msg.Topic, sendErr)
	}

	s.log.Info(ctx, "Notification sent", map[string]interface{}{
		"event_id": ev.eventID, "type": msg.Topic, "order_id": ev.data.OrderID, "user_id": ev.userID,
	})
	return nil
}

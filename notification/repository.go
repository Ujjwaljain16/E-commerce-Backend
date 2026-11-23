package notification

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Delivery statuses stored with each notification.
const (
	StatusSent   = "SENT"
	StatusFailed = "FAILED"
)

// ChannelEmail is the only delivery channel so far.
const ChannelEmail = "EMAIL"

// Notification is one message sent (or attempted) in response to an event.
type Notification struct {
	ID        string
	UserID    string
	EventID   string
	Type      string
	Channel   string
	Recipient string
	Subject   string
	Body      string
	Status    string
	Error     string
	CreatedAt time.Time
}

// Repository stores notification history.
type Repository interface {
	// WasSent reports whether the event already produced a delivered notification.
	WasSent(ctx context.Context, eventID string) (bool, error)
	// Save records a delivery attempt. Each event keeps a single row, updated on retries until it is SENT.
	Save(ctx context.Context, n *Notification) error
	ListByUser(ctx context.Context, userID string, page, pageSize int) ([]*Notification, int, error)
}

type postgresRepository struct {
	db *sql.DB
}

// NewRepository returns a PostgreSQL-backed Repository.
func NewRepository(db *sql.DB) Repository {
	return &postgresRepository{db: db}
}

func (r *postgresRepository) WasSent(ctx context.Context, eventID string) (bool, error) {
	var sent bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM notifications WHERE event_id = $1 AND status = $2)`,
		eventID, StatusSent).Scan(&sent)
	if err != nil {
		return false, fmt.Errorf("check notification: %w", err)
	}
	return sent, nil
}

func (r *postgresRepository) Save(ctx context.Context, n *Notification) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO notifications (id, user_id, event_id, type, channel, recipient, subject, body, status, error)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, ''))
		ON CONFLICT (event_id) DO UPDATE SET
			recipient = EXCLUDED.recipient,
			subject = EXCLUDED.subject,
			body = EXCLUDED.body,
			status = EXCLUDED.status,
			error = EXCLUDED.error
		WHERE notifications.status <> 'SENT'`,
		n.ID, n.UserID, n.EventID, n.Type, n.Channel, n.Recipient, n.Subject, n.Body, n.Status, n.Error)
	if err != nil {
		return fmt.Errorf("save notification: %w", err)
	}
	return nil
}

func (r *postgresRepository) ListByUser(ctx context.Context, userID string, page, pageSize int) ([]*Notification, int, error) {
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count notifications: %w", err)
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT id, user_id, event_id, type, channel, recipient, subject, body, status, COALESCE(error, ''), created_at
		FROM notifications WHERE user_id = $1
		ORDER BY created_at DESC, id LIMIT $2 OFFSET $3`,
		userID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list notifications: %w", err)
	}
	defer rows.Close()

	out := []*Notification{}
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.EventID, &n.Type, &n.Channel, &n.Recipient,
			&n.Subject, &n.Body, &n.Status, &n.Error, &n.CreatedAt); err != nil {
			return nil, 0, fmt.Errorf("scan notification: %w", err)
		}
		out = append(out, &n)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list notifications: %w", err)
	}
	return out, total, nil
}

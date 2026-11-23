package payment

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/outbox"
)

// OutboxTable is where payment events wait for the relay to publish them.
const OutboxTable = "payment_outbox"

var (
	// ErrPaymentNotFound is returned when no payment matches the lookup.
	ErrPaymentNotFound = errors.New("payment not found")
	// ErrStatusChanged is returned when a transition loses a race with another update.
	ErrStatusChanged = errors.New("payment status changed concurrently")
)

// Status mirrors pb.PaymentStatus so values stored in the database match the proto enum.
type Status int

// Payment statuses.
const (
	StatusPending   Status = 1
	StatusSucceeded Status = 2
	StatusFailed    Status = 3
	StatusRefunded  Status = 4
	StatusCancelled Status = 5
)

// Payment is a single charge attempt for an order.
type Payment struct {
	ID             string
	OrderID        string
	UserID         string
	AmountCents    int64
	Currency       string
	Status         Status
	Method         string
	ProviderRef    string
	FailureReason  string
	IdempotencyKey string
	// Provider is ProviderMock or ProviderRazorpay.
	Provider string
	// ProviderOrderID is the Razorpay order the customer pays in Checkout; empty until created.
	ProviderOrderID string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Payment providers.
const (
	ProviderMock     = "mock"
	ProviderRazorpay = "razorpay"
)

// Transition moves a payment from one status to another and queues an event in the same transaction.
// An empty Topic changes the status without an event.
type Transition struct {
	ID            string
	From          Status
	To            Status
	ProviderRef   string
	FailureReason string
	Topic         string
	Event         interface{}
}

// Repository is the payment storage used by Service.
type Repository interface {
	// Insert stores p unless a payment with the same order or idempotency key exists.
	// It returns the stored payment and whether this call created it.
	Insert(ctx context.Context, p *Payment) (*Payment, bool, error)
	GetByID(ctx context.Context, id string) (*Payment, error)
	GetByOrderID(ctx context.Context, orderID string) (*Payment, error)
	GetByProviderOrderID(ctx context.Context, providerOrderID string) (*Payment, error)
	ListByUser(ctx context.Context, userID string, page, pageSize int) ([]*Payment, int, error)
	Apply(ctx context.Context, t Transition) (*Payment, error)
	// SetProviderOrderID records the provider's order on a PENDING payment that has none yet and
	// returns the stored payment, which keeps the existing ID if another attempt saved one first.
	SetProviderOrderID(ctx context.Context, id, providerOrderID string) (*Payment, error)
	// ListPendingBefore returns up to limit PENDING payments of provider created before cutoff, oldest first.
	ListPendingBefore(ctx context.Context, provider string, cutoff time.Time, limit int) ([]*Payment, error)
}

const paymentColumns = `id, order_id, user_id, amount_cents, currency, status, COALESCE(method, ''),
	COALESCE(provider_ref, ''), COALESCE(failure_reason, ''), idempotency_key, provider,
	COALESCE(provider_order_id, ''), created_at, updated_at`

type postgresRepository struct {
	db *sql.DB
}

// NewRepository returns a PostgreSQL-backed Repository.
func NewRepository(db *sql.DB) Repository {
	return &postgresRepository{db: db}
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanPayment(row rowScanner) (*Payment, error) {
	var p Payment
	err := row.Scan(&p.ID, &p.OrderID, &p.UserID, &p.AmountCents, &p.Currency, &p.Status, &p.Method,
		&p.ProviderRef, &p.FailureReason, &p.IdempotencyKey, &p.Provider, &p.ProviderOrderID, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (r *postgresRepository) Insert(ctx context.Context, p *Payment) (*Payment, bool, error) {
	row := r.db.QueryRowContext(ctx, `
		INSERT INTO payments (id, order_id, user_id, amount_cents, currency, status, method, failure_reason, idempotency_key, provider)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), NULLIF($8, ''), $9, $10)
		ON CONFLICT DO NOTHING
		RETURNING `+paymentColumns,
		p.ID, p.OrderID, p.UserID, p.AmountCents, p.Currency, p.Status, p.Method, p.FailureReason, p.IdempotencyKey,
		providerOrMock(p.Provider))
	created, err := scanPayment(row)
	if err == nil {
		return created, true, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, fmt.Errorf("insert payment: %w", err)
	}

	// Nothing returned means a payment already exists for this key or order.
	existing, err := scanPayment(r.db.QueryRowContext(ctx,
		`SELECT `+paymentColumns+` FROM payments WHERE idempotency_key = $1 OR order_id = $2
		ORDER BY (idempotency_key = $1) DESC LIMIT 1`,
		p.IdempotencyKey, p.OrderID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, fmt.Errorf("payment conflict but no existing row for order %s", p.OrderID)
		}
		return nil, false, fmt.Errorf("load existing payment: %w", err)
	}
	return existing, false, nil
}

func (r *postgresRepository) GetByID(ctx context.Context, id string) (*Payment, error) {
	return r.getOne(ctx, `SELECT `+paymentColumns+` FROM payments WHERE id = $1`, id)
}

func (r *postgresRepository) GetByOrderID(ctx context.Context, orderID string) (*Payment, error) {
	return r.getOne(ctx, `SELECT `+paymentColumns+` FROM payments WHERE order_id = $1`, orderID)
}

func (r *postgresRepository) GetByProviderOrderID(ctx context.Context, providerOrderID string) (*Payment, error) {
	return r.getOne(ctx, `SELECT `+paymentColumns+` FROM payments WHERE provider_order_id = $1`, providerOrderID)
}

func (r *postgresRepository) SetProviderOrderID(ctx context.Context, id, providerOrderID string) (*Payment, error) {
	p, err := scanPayment(r.db.QueryRowContext(ctx, `
		UPDATE payments SET provider_order_id = $2, updated_at = NOW()
		WHERE id = $1 AND status = $3 AND provider_order_id IS NULL
		RETURNING `+paymentColumns,
		id, providerOrderID, StatusPending))
	if errors.Is(err, sql.ErrNoRows) {
		return r.GetByID(ctx, id)
	}
	if err != nil {
		return nil, fmt.Errorf("set provider order: %w", err)
	}
	return p, nil
}

func (r *postgresRepository) ListPendingBefore(ctx context.Context, provider string, cutoff time.Time, limit int) ([]*Payment, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT `+paymentColumns+` FROM payments
		WHERE status = $1 AND provider = $2 AND created_at < $3
		ORDER BY created_at LIMIT $4`,
		StatusPending, provider, cutoff, limit)
	if err != nil {
		return nil, fmt.Errorf("list pending payments: %w", err)
	}
	defer rows.Close()

	var payments []*Payment
	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return nil, fmt.Errorf("scan payment: %w", err)
		}
		payments = append(payments, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list pending payments: %w", err)
	}
	return payments, nil
}

func providerOrMock(provider string) string {
	if provider == "" {
		return ProviderMock
	}
	return provider
}

func (r *postgresRepository) getOne(ctx context.Context, query, arg string) (*Payment, error) {
	p, err := scanPayment(r.db.QueryRowContext(ctx, query, arg))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrPaymentNotFound
		}
		return nil, fmt.Errorf("get payment: %w", err)
	}
	return p, nil
}

func (r *postgresRepository) ListByUser(ctx context.Context, userID string, page, pageSize int) ([]*Payment, int, error) {
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM payments WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count payments: %w", err)
	}

	rows, err := r.db.QueryContext(ctx,
		`SELECT `+paymentColumns+` FROM payments WHERE user_id = $1 ORDER BY created_at DESC, id LIMIT $2 OFFSET $3`,
		userID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list payments: %w", err)
	}
	defer rows.Close()

	payments := []*Payment{}
	for rows.Next() {
		p, err := scanPayment(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan payment: %w", err)
		}
		payments = append(payments, p)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("list payments: %w", err)
	}
	return payments, total, nil
}

func (r *postgresRepository) Apply(ctx context.Context, t Transition) (*Payment, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	// The status guard makes concurrent transitions (e.g. two refunds) lose cleanly instead of both emitting events.
	updated, err := scanPayment(tx.QueryRowContext(ctx, `
		UPDATE payments
		SET status = $1,
			provider_ref = COALESCE(NULLIF($2, ''), provider_ref),
			failure_reason = COALESCE(NULLIF($3, ''), failure_reason),
			updated_at = NOW()
		WHERE id = $4 AND status = $5
		RETURNING `+paymentColumns,
		t.To, t.ProviderRef, t.FailureReason, t.ID, t.From))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrStatusChanged
		}
		return nil, fmt.Errorf("update payment: %w", err)
	}

	if t.Topic != "" {
		if err := outbox.Add(ctx, tx, OutboxTable, t.Topic, updated.OrderID, t.Event); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit payment update: %w", err)
	}
	return updated, nil
}

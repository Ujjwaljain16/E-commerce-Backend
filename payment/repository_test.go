package payment

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var paymentRowColumns = []string{
	"id", "order_id", "user_id", "amount_cents", "currency", "status", "method",
	"provider_ref", "failure_reason", "idempotency_key", "provider", "provider_order_id", "created_at", "updated_at",
}

const (
	testPaymentID = "11111111-1111-1111-1111-111111111111"
	testOrderID   = "22222222-2222-2222-2222-222222222222"
	testUserID    = "33333333-3333-3333-3333-333333333333"
)

func newMockRepo(t *testing.T) (Repository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return NewRepository(db), mock
}

func paymentRow(status Status, providerRef string) *sqlmock.Rows {
	now := time.Now()
	return sqlmock.NewRows(paymentRowColumns).AddRow(
		testPaymentID, testOrderID, testUserID, int64(4999), "USD", int64(status), "card",
		providerRef, "", testOrderID, ProviderMock, "", now, now)
}

func TestRepositoryInsertCreatesPayment(t *testing.T) {
	repo, mock := newMockRepo(t)

	mock.ExpectQuery(`INSERT INTO payments .* ON CONFLICT DO NOTHING`).
		WithArgs(testPaymentID, testOrderID, testUserID, int64(4999), "USD", StatusPending, "card", "", testOrderID, ProviderMock).
		WillReturnRows(paymentRow(StatusPending, ""))

	p, created, err := repo.Insert(context.Background(), &Payment{
		ID: testPaymentID, OrderID: testOrderID, UserID: testUserID, AmountCents: 4999, Currency: "USD",
		Status: StatusPending, Method: "card", IdempotencyKey: testOrderID,
	})
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, int64(4999), p.AmountCents)
	assert.Equal(t, StatusPending, p.Status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRepositoryInsertReturnsExistingOnConflict(t *testing.T) {
	repo, mock := newMockRepo(t)

	mock.ExpectQuery(`INSERT INTO payments`).WillReturnRows(sqlmock.NewRows(paymentRowColumns))
	mock.ExpectQuery(`SELECT .* FROM payments WHERE idempotency_key = \$1 OR order_id = \$2`).
		WithArgs(testOrderID, testOrderID).
		WillReturnRows(paymentRow(StatusSucceeded, "mock_ref"))

	p, created, err := repo.Insert(context.Background(), &Payment{
		ID: "another-id", OrderID: testOrderID, UserID: testUserID, AmountCents: 4999, Currency: "USD",
		Status: StatusPending, IdempotencyKey: testOrderID,
	})
	require.NoError(t, err)
	assert.False(t, created)
	assert.Equal(t, testPaymentID, p.ID)
	assert.Equal(t, StatusSucceeded, p.Status)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRepositoryGetByIDNotFound(t *testing.T) {
	repo, mock := newMockRepo(t)

	mock.ExpectQuery(`SELECT .* FROM payments WHERE id = \$1`).
		WithArgs(testPaymentID).
		WillReturnError(sql.ErrNoRows)

	_, err := repo.GetByID(context.Background(), testPaymentID)
	assert.ErrorIs(t, err, ErrPaymentNotFound)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRepositoryApplyUpdatesAndQueuesEventInOneTransaction(t *testing.T) {
	repo, mock := newMockRepo(t)

	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE payments`).
		WithArgs(StatusSucceeded, "mock_ref", "", testPaymentID, StatusPending).
		WillReturnRows(paymentRow(StatusSucceeded, "mock_ref"))
	mock.ExpectExec(`INSERT INTO payment_outbox`).
		WithArgs("payment.succeeded", testOrderID, sqlmock.AnyArg()).
		WillReturnResult(sqlmock.NewResult(1, 1))
	mock.ExpectCommit()

	p, err := repo.Apply(context.Background(), Transition{
		ID: testPaymentID, From: StatusPending, To: StatusSucceeded, ProviderRef: "mock_ref",
		Topic: "payment.succeeded", Event: map[string]string{"payment_id": testPaymentID},
	})
	require.NoError(t, err)
	assert.Equal(t, StatusSucceeded, p.Status)
	assert.Equal(t, "mock_ref", p.ProviderRef)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRepositoryApplyRollsBackWhenStatusChanged(t *testing.T) {
	repo, mock := newMockRepo(t)

	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE payments`).WillReturnRows(sqlmock.NewRows(paymentRowColumns))
	mock.ExpectRollback()

	_, err := repo.Apply(context.Background(), Transition{
		ID: testPaymentID, From: StatusSucceeded, To: StatusRefunded, Topic: "payment.refunded",
	})
	assert.ErrorIs(t, err, ErrStatusChanged)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRepositoryListByUser(t *testing.T) {
	repo, mock := newMockRepo(t)

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM payments WHERE user_id = \$1`).
		WithArgs(testUserID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(11))
	mock.ExpectQuery(`SELECT .* FROM payments WHERE user_id = \$1 ORDER BY created_at DESC`).
		WithArgs(testUserID, 10, 10).
		WillReturnRows(paymentRow(StatusSucceeded, "mock_ref"))

	payments, total, err := repo.ListByUser(context.Background(), testUserID, 2, 10)
	require.NoError(t, err)
	assert.Equal(t, 11, total)
	require.Len(t, payments, 1)
	assert.Equal(t, testPaymentID, payments[0].ID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

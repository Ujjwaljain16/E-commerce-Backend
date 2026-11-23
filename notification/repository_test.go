package notification

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testUserID  = "33333333-3333-3333-3333-333333333333"
	testOrderID = "22222222-2222-2222-2222-222222222222"
)

func newMockRepo(t *testing.T) (Repository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	return NewRepository(db), mock
}

func TestRepositoryWasSent(t *testing.T) {
	repo, mock := newMockRepo(t)

	mock.ExpectQuery(`SELECT EXISTS \(SELECT 1 FROM notifications WHERE event_id = \$1 AND status = \$2\)`).
		WithArgs("evt-1", StatusSent).
		WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

	sent, err := repo.WasSent(context.Background(), "evt-1")
	require.NoError(t, err)
	assert.True(t, sent)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRepositorySaveUpsertsByEventID(t *testing.T) {
	repo, mock := newMockRepo(t)
	n := &Notification{
		ID: "n-1", UserID: testUserID, EventID: "evt-1", Type: "order.created", Channel: ChannelEmail,
		Recipient: "a@example.com", Subject: "s", Body: "b", Status: StatusFailed, Error: "smtp down",
	}

	mock.ExpectExec(`INSERT INTO notifications .* ON CONFLICT \(event_id\) DO UPDATE .* WHERE notifications.status <> 'SENT'`).
		WithArgs(n.ID, n.UserID, n.EventID, n.Type, n.Channel, n.Recipient, n.Subject, n.Body, n.Status, n.Error).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, repo.Save(context.Background(), n))
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestRepositoryListByUser(t *testing.T) {
	repo, mock := newMockRepo(t)
	now := time.Now()

	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM notifications WHERE user_id = \$1`).
		WithArgs(testUserID).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(3))
	mock.ExpectQuery(`SELECT .* FROM notifications WHERE user_id = \$1\s+ORDER BY created_at DESC`).
		WithArgs(testUserID, 2, 2).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "user_id", "event_id", "type", "channel", "recipient", "subject", "body", "status", "error", "created_at",
		}).AddRow("n-3", testUserID, "evt-3", "order.cancelled", ChannelEmail, "a@example.com", "s", "b", StatusSent, "", now))

	list, total, err := repo.ListByUser(context.Background(), testUserID, 2, 2)
	require.NoError(t, err)
	assert.Equal(t, 3, total)
	require.Len(t, list, 1)
	assert.Equal(t, "evt-3", list[0].EventID)
	assert.NoError(t, mock.ExpectationsWereMet())
}

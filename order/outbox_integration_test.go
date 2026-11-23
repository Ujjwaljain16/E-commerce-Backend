package order

import (
	"context"
	"testing"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/outbox"
	"github.com/stretchr/testify/require"
)

func TestOutboxPruneOnRealPostgres(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	ctx := context.Background()

	_, err := db.ExecContext(ctx, `INSERT INTO order_outbox (topic, message_key, payload, published_at) VALUES
		('order.created', 'old', '{}', NOW() - INTERVAL '8 days'),
		('order.created', 'recent', '{}', NOW() - INTERVAL '1 day'),
		('order.created', 'pending', '{}', NULL)`)
	require.NoError(t, err)

	n, err := outbox.NewRelay(db, OutboxTable, nil, logger.New("test")).Prune(ctx, 7*24*time.Hour)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	var left []string
	rows, err := db.QueryContext(ctx, `SELECT message_key FROM order_outbox ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()
	for rows.Next() {
		var k string
		require.NoError(t, rows.Scan(&k))
		left = append(left, k)
	}
	require.Equal(t, []string{"recent", "pending"}, left)
}

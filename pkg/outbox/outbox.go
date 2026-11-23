// Package outbox implements the transactional outbox pattern: events are written to a table in the
// same transaction as the state change, and a relay publishes them to Kafka afterwards. This avoids
// losing events (or publishing events for rolled-back changes) when the DB and Kafka disagree.
//
// Expected table (name is configurable per service):
//
//	CREATE TABLE <name> (
//	    id BIGSERIAL PRIMARY KEY,
//	    topic TEXT NOT NULL,
//	    message_key TEXT NOT NULL,
//	    payload JSONB NOT NULL,
//	    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
//	    published_at TIMESTAMPTZ
//	);
//	CREATE INDEX ON <name> (id) WHERE published_at IS NULL;
package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/kafka"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
)

// Execer is satisfied by *sql.Tx and *sql.DB.
type Execer interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// Add stores an event in the outbox table using the caller's transaction.
func Add(ctx context.Context, tx Execer, table, topic, key string, event interface{}) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal outbox event: %w", err)
	}
	// table is a compile-time constant chosen by each service, never user input.
	q := fmt.Sprintf(`INSERT INTO %s (topic, message_key, payload) VALUES ($1, $2, $3)`, table)
	if _, err := tx.ExecContext(ctx, q, topic, key, payload); err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}
	return nil
}

// Relay polls the outbox table and publishes pending events in insertion order.
type Relay struct {
	db        *sql.DB
	table     string
	pub       kafka.Publisher
	log       *logger.Logger
	interval  time.Duration
	batchSize int
}

// NewRelay creates a relay for the given table.
func NewRelay(db *sql.DB, table string, pub kafka.Publisher, log *logger.Logger) *Relay {
	return &Relay{db: db, table: table, pub: pub, log: log, interval: 500 * time.Millisecond, batchSize: 100}
}

// Run publishes pending events until ctx is cancelled.
func (r *Relay) Run(ctx context.Context) {
	t := time.NewTicker(r.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for {
				n, err := r.flush(ctx)
				if err != nil {
					if ctx.Err() == nil {
						r.log.Error(ctx, "outbox flush failed", map[string]interface{}{"error": err.Error(), "table": r.table})
					}
					break
				}
				if n < r.batchSize {
					break
				}
			}
		}
	}
}

// flush publishes one batch. FOR UPDATE SKIP LOCKED lets several replicas run relays safely.
func (r *Relay) flush(ctx context.Context) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	rows, err := tx.QueryContext(ctx, fmt.Sprintf(
		`SELECT id, topic, message_key, payload FROM %s WHERE published_at IS NULL ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED`,
		r.table), r.batchSize)
	if err != nil {
		return 0, err
	}
	type pending struct {
		id         int64
		topic, key string
		payload    []byte
	}
	var batch []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.topic, &p.key, &p.payload); err != nil {
			rows.Close()
			return 0, err
		}
		batch = append(batch, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	published := 0
	for _, p := range batch {
		if err := r.pub.Publish(ctx, p.topic, p.key, p.payload); err != nil {
			// Stop at the first failure to keep per-key ordering; already published rows are still marked.
			r.log.Warn(ctx, "outbox publish failed", map[string]interface{}{"error": err.Error(), "topic": p.topic})
			break
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET published_at = NOW() WHERE id = $1`, r.table), p.id); err != nil {
			return published, err
		}
		published++
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return published, nil
}

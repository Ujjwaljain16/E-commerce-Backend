package outbox

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/kafka"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
)

type fakePublisher struct {
	sent    []string
	failOn  string
	failErr error
}

func (f *fakePublisher) Publish(_ context.Context, topic, key string, _ []byte) error {
	if key == f.failOn {
		return f.failErr
	}
	f.sent = append(f.sent, topic+"/"+key)
	return nil
}

func newRelay(t *testing.T, pub *fakePublisher) (*Relay, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewRelay(db, "test_outbox", pub, logger.New("test")), mock
}

func TestAdd_WritesSerializedEvent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mock.ExpectExec(`INSERT INTO test_outbox \(topic, message_key, payload\)`).
		WithArgs("order.created", "o1", []byte(`{"id":"o1"}`)).
		WillReturnResult(sqlmock.NewResult(1, 1))

	if err := Add(context.Background(), db, "test_outbox", "order.created", "o1", map[string]string{"id": "o1"}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFlush_PublishesInOrderAndMarksSent(t *testing.T) {
	pub := &fakePublisher{}
	r, mock := newRelay(t, pub)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id, topic, message_key, payload FROM test_outbox WHERE published_at IS NULL ORDER BY id`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "topic", "message_key", "payload"}).
			AddRow(1, "order.created", "a", []byte(`{}`)).
			AddRow(2, "order.cancelled", "a", []byte(`{}`)))
	mock.ExpectExec(`UPDATE test_outbox SET published_at = NOW\(\) WHERE id = \$1`).WithArgs(1).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE test_outbox SET published_at = NOW\(\) WHERE id = \$1`).WithArgs(2).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	n, err := r.flush(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("flush = %d, %v", n, err)
	}
	if len(pub.sent) != 2 || pub.sent[0] != "order.created/a" || pub.sent[1] != "order.cancelled/a" {
		t.Fatalf("unexpected publish order %v", pub.sent)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFlush_StopsAtFirstFailureToKeepOrder(t *testing.T) {
	pub := &fakePublisher{failOn: "b", failErr: errors.New("broker down")}
	r, mock := newRelay(t, pub)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id, topic, message_key, payload FROM test_outbox`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "topic", "message_key", "payload"}).
			AddRow(1, "t", "a", []byte(`{}`)).
			AddRow(2, "t", "b", []byte(`{}`)).
			AddRow(3, "t", "c", []byte(`{}`)))
	mock.ExpectExec(`UPDATE test_outbox SET published_at`).WithArgs(1).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	n, err := r.flush(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("flush = %d, %v", n, err)
	}
	if len(pub.sent) != 1 {
		t.Fatalf("events after a failed one must wait for the next round, sent %v", pub.sent)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestPrune_DeletesInBatchesUntilDone(t *testing.T) {
	r, mock := newRelay(t, &fakePublisher{})
	week := 7 * 24 * time.Hour

	mock.ExpectExec(`DELETE FROM test_outbox WHERE id IN`).WithArgs(week.Seconds()).WillReturnResult(sqlmock.NewResult(0, 5000))
	mock.ExpectExec(`DELETE FROM test_outbox WHERE id IN`).WithArgs(week.Seconds()).WillReturnResult(sqlmock.NewResult(0, 12))

	n, err := r.Prune(context.Background(), week)
	if err != nil || n != 5012 {
		t.Fatalf("prune = %d, %v", n, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type fakeBatchPublisher struct {
	fakePublisher
	batches [][]kafka.Message
	err     error
}

func (f *fakeBatchPublisher) PublishBatch(_ context.Context, msgs []kafka.Message) error {
	if f.err != nil {
		return f.err
	}
	f.batches = append(f.batches, msgs)
	return nil
}

func TestFlush_BatchPublisherSendsOneBatchAndMarksAll(t *testing.T) {
	pub := &fakeBatchPublisher{}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := NewRelay(db, "test_outbox", pub, logger.New("test"))

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id, topic, message_key, payload FROM test_outbox`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "topic", "message_key", "payload"}).
			AddRow(1, "order.created", "a", []byte(`{}`)).
			AddRow(2, "order.created", "b", []byte(`{}`)))
	mock.ExpectExec(`UPDATE test_outbox SET published_at = NOW\(\) WHERE id = ANY\(\$1\)`).WillReturnResult(sqlmock.NewResult(0, 2))
	mock.ExpectCommit()

	n, err := r.flush(context.Background())
	if err != nil || n != 2 {
		t.Fatalf("flush = %d, %v", n, err)
	}
	if len(pub.batches) != 1 || len(pub.batches[0]) != 2 || pub.batches[0][0].Key != "a" {
		t.Fatalf("expected one ordered batch of 2, got %+v", pub.batches)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestFlush_FailedBatchMarksNothing(t *testing.T) {
	pub := &fakeBatchPublisher{err: errors.New("broker down")}
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := NewRelay(db, "test_outbox", pub, logger.New("test"))

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id, topic, message_key, payload FROM test_outbox`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "topic", "message_key", "payload"}).AddRow(1, "t", "a", []byte(`{}`)))
	mock.ExpectRollback()

	if n, err := r.flush(context.Background()); err != nil || n != 0 {
		t.Fatalf("flush = %d, %v", n, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

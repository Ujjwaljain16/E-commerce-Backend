package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/events"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/kafka"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	kafkago "github.com/segmentio/kafka-go"
)

func newMockRepo(t *testing.T) (*postgresRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return NewPostgresRepository(db, logger.New("test")).(*postgresRepository), mock
}

func TestCommitReservation_ReservedBecomesCommitted(t *testing.T) {
	repo, mock := newMockRepo(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT status FROM stock_reservations WHERE order_id = \$1 FOR UPDATE`).
		WithArgs(testOrderID).WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("RESERVED"))
	mock.ExpectExec(`UPDATE stock_reservations SET status = 'COMMITTED'`).WithArgs(testOrderID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	touched, err := repo.CommitReservation(context.Background(), testOrderID)
	if err != nil || len(touched) != 0 {
		t.Fatalf("commit = %v, %v", touched, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCommitReservation_CancelledFirstStaysReleased(t *testing.T) {
	repo, mock := newMockRepo(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT status FROM stock_reservations`).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("RELEASED"))
	mock.ExpectCommit()

	if _, err := repo.CommitReservation(context.Background(), testOrderID); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("a cancelled reservation must not be committed: %v", err)
	}
}

func TestCommitReservation_ExpiredIsRestoredWhenStockAllows(t *testing.T) {
	repo, mock := newMockRepo(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT status FROM stock_reservations`).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("EXPIRED"))
	mock.ExpectQuery(`SELECT product_id, product_name, quantity, unit_price FROM stock_reservation_items`).
		WillReturnRows(sqlmock.NewRows([]string{"product_id", "product_name", "quantity", "unit_price"}).AddRow(productA, "A", 2, 5.0))
	mock.ExpectExec(`UPDATE products SET stock = stock - \$2 WHERE id = \$1 AND stock >= \$2`).
		WithArgs(productA, int32(2)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE stock_reservations SET status = 'COMMITTED'`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	touched, err := repo.CommitReservation(context.Background(), testOrderID)
	if err != nil || len(touched) != 1 || touched[0] != productA {
		t.Fatalf("commit = %v, %v", touched, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestCommitReservation_ExpiredAndSoldOutIsLost(t *testing.T) {
	repo, mock := newMockRepo(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT status FROM stock_reservations`).
		WillReturnRows(sqlmock.NewRows([]string{"status"}).AddRow("EXPIRED"))
	mock.ExpectQuery(`SELECT product_id, product_name, quantity, unit_price FROM stock_reservation_items`).
		WillReturnRows(sqlmock.NewRows([]string{"product_id", "product_name", "quantity", "unit_price"}).AddRow(productA, "A", 2, 5.0))
	mock.ExpectExec(`UPDATE products SET stock = stock - \$2`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()

	_, err := repo.CommitReservation(context.Background(), testOrderID)
	if !errors.Is(err, ErrReservationLost) {
		t.Fatalf("expected ErrReservationLost, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestExpireReservation_OnlyTouchesReservedRows(t *testing.T) {
	repo, mock := newMockRepo(t)
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE stock_reservations SET status = \$2`).
		WithArgs(testOrderID, "EXPIRED", sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT product_id, product_name, quantity, unit_price FROM stock_reservation_items`).
		WillReturnRows(sqlmock.NewRows([]string{"product_id", "product_name", "quantity", "unit_price"}).AddRow(productA, "A", 3, 5.0))
	mock.ExpectExec(`UPDATE products SET stock = stock \+ \$2`).WithArgs(productA, int32(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	ids, err := repo.ExpireReservation(context.Background(), testOrderID)
	if err != nil || len(ids) != 1 {
		t.Fatalf("expire = %v, %v", ids, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestSweeper_ExpiresStaleReservations(t *testing.T) {
	var expired []string
	repo := &MockRepository{
		ExpiredFunc: func(_ context.Context, ttl time.Duration, _ int) ([]string, error) {
			if ttl != 15*time.Minute {
				t.Fatalf("unexpected ttl %v", ttl)
			}
			return []string{"o1", "o2"}, nil
		},
		ExpireFunc: func(_ context.Context, id string) ([]string, error) {
			expired = append(expired, id)
			if id == "o2" {
				return nil, nil // raced with a commit: nothing released
			}
			return []string{productA}, nil
		},
	}
	n := NewReservationSweeper(repo, logger.New("test"), 15*time.Minute).SweepOnce(context.Background())
	if n != 1 || len(expired) != 2 {
		t.Fatalf("released=%d expired=%v", n, expired)
	}
}

func TestOrderEventHandler_CreatedCommits(t *testing.T) {
	var committed []string
	h := NewOrderEventHandler(&MockRepository{CommitFunc: func(_ context.Context, id string) ([]string, error) {
		committed = append(committed, id)
		return nil, nil
	}}, logger.New("test"))
	msg := kafkago.Message{Topic: events.TopicOrderCreated, Value: []byte(`{"order_id":"` + testOrderID + `"}`)}
	if err := h.Handle(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	if len(committed) != 1 {
		t.Fatalf("expected a commit, got %v", committed)
	}
}

func TestOrderEventHandler_LostReservationIsNotRetried(t *testing.T) {
	h := NewOrderEventHandler(&MockRepository{CommitFunc: func(context.Context, string) ([]string, error) {
		return nil, &StockError{ProductID: productA, Err: ErrReservationLost}
	}}, logger.New("test"))
	msg := kafkago.Message{Topic: events.TopicOrderCreated, Value: []byte(`{"order_id":"` + testOrderID + `"}`)}
	if err := h.Handle(context.Background(), msg); !errors.Is(err, kafka.ErrSkip) {
		t.Fatalf("expected ErrSkip, got %v", err)
	}
}

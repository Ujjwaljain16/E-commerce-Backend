package catalog

import (
	"context"
	"testing"
	"time"

	"errors"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Ujjwaljain16/E-commerce-Backend/catalog/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/kafka"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	kafkago "github.com/segmentio/kafka-go"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	testOrderID = "8f8a9a4e-2b0c-4a59-9d55-0d3c7f6f6b11"
	productA    = "11111111-1111-4111-8111-111111111111"
	productB    = "22222222-2222-4222-8222-222222222222"
)

func TestReserveStock_MergesDuplicateLines(t *testing.T) {
	var got []StockItem
	svc := setupService(&MockRepository{ReserveFunc: func(_ context.Context, _ string, items []StockItem) ([]ReservedItem, error) {
		got = items
		return []ReservedItem{{ProductID: productA, ProductName: "A", Quantity: 3, UnitPrice: 9.5}}, nil
	}})

	resp, err := svc.ReserveStock(context.Background(), &pb.ReserveStockRequest{
		OrderId: testOrderID,
		Items:   []*pb.StockItem{{ProductId: productA, Quantity: 1}, {ProductId: productA, Quantity: 2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Quantity != 3 {
		t.Fatalf("expected one merged line with quantity 3, got %+v", got)
	}
	if resp.Items[0].UnitPrice != 9.5 || resp.Items[0].ProductName != "A" {
		t.Fatalf("unexpected response %+v", resp.Items[0])
	}
}

func TestReserveStock_Validation(t *testing.T) {
	svc := setupService(&MockRepository{})
	cases := map[string]*pb.ReserveStockRequest{
		"bad order id":   {OrderId: "nope", Items: []*pb.StockItem{{ProductId: productA, Quantity: 1}}},
		"no items":       {OrderId: testOrderID},
		"bad product id": {OrderId: testOrderID, Items: []*pb.StockItem{{ProductId: "x", Quantity: 1}}},
		"zero quantity":  {OrderId: testOrderID, Items: []*pb.StockItem{{ProductId: productA}}},
	}
	for name, req := range cases {
		if _, err := svc.ReserveStock(context.Background(), req); status.Code(err) != codes.InvalidArgument {
			t.Errorf("%s: expected InvalidArgument, got %v", name, err)
		}
	}
}

func TestReserveStock_ErrorMapping(t *testing.T) {
	for want, repoErr := range map[codes.Code]error{
		codes.NotFound:           &StockError{ProductID: productA, Err: ErrProductNotFound},
		codes.FailedPrecondition: &StockError{ProductID: productA, Err: ErrInsufficientStock},
	} {
		svc := setupService(&MockRepository{ReserveFunc: func(context.Context, string, []StockItem) ([]ReservedItem, error) {
			return nil, repoErr
		}})
		_, err := svc.ReserveStock(context.Background(), &pb.ReserveStockRequest{
			OrderId: testOrderID, Items: []*pb.StockItem{{ProductId: productA, Quantity: 1}},
		})
		if status.Code(err) != want {
			t.Errorf("expected %v, got %v", want, err)
		}
	}
}

func TestRepositoryReserveStock_LocksInProductOrder(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewPostgresRepository(db, logger.New("test"))

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO stock_reservations`).WithArgs(testOrderID).WillReturnResult(sqlmock.NewResult(0, 1))
	for _, id := range []string{productA, productB} {
		mock.ExpectQuery(`UPDATE products SET stock = stock - \$2`).WithArgs(id, int32(2)).
			WillReturnRows(sqlmock.NewRows([]string{"name", "price"}).AddRow("p", 5.0))
		mock.ExpectExec(`INSERT INTO stock_reservation_items`).WillReturnResult(sqlmock.NewResult(0, 1))
	}
	mock.ExpectCommit()

	items, err := repo.ReserveStock(context.Background(), testOrderID,
		[]StockItem{{ProductID: productB, Quantity: 2}, {ProductID: productA, Quantity: 2}})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].ProductID != productA {
		t.Fatalf("expected items sorted by product id, got %+v", items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryReserveStock_InsufficientStockRollsBack(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewPostgresRepository(db, logger.New("test"))

	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO stock_reservations`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`UPDATE products SET stock`).WillReturnRows(sqlmock.NewRows([]string{"name", "price"}))
	mock.ExpectQuery(`SELECT EXISTS`).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))
	mock.ExpectRollback()

	_, err = repo.ReserveStock(context.Background(), testOrderID, []StockItem{{ProductID: productA, Quantity: 99}})
	if err == nil || !isStockErr(err, ErrInsufficientStock) {
		t.Fatalf("expected insufficient stock, got %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestRepositoryReleaseStock_IsIdempotent(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repo := NewPostgresRepository(db, logger.New("test"))

	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE stock_reservations SET status = 'RELEASED'`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	ids, err := repo.ReleaseStock(context.Background(), testOrderID)
	if err != nil || len(ids) != 0 {
		t.Fatalf("expected no-op release, got %v %v", ids, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func isStockErr(err, target error) bool {
	se, ok := err.(*StockError)
	return ok && se.Err == target
}

type memCache struct{ m map[string][]byte }

func (c *memCache) Get(_ context.Context, k string) ([]byte, bool, error) {
	v, ok := c.m[k]
	return v, ok, nil
}

func (c *memCache) Set(_ context.Context, k string, v []byte, _ time.Duration) error {
	c.m[k] = v
	return nil
}

func (c *memCache) Delete(_ context.Context, keys ...string) error {
	for _, k := range keys {
		delete(c.m, k)
	}
	return nil
}

func TestCachedRepository_ReadThroughAndInvalidate(t *testing.T) {
	calls := 0
	base := &MockRepository{
		GetByIDFunc: func(_ context.Context, id string) (*Product, error) {
			calls++
			return &Product{ID: id, Name: "Mug", Stock: 5}, nil
		},
		ReserveFunc: func(context.Context, string, []StockItem) ([]ReservedItem, error) { return nil, nil },
	}
	c := &memCache{m: map[string][]byte{}}
	repo := NewCachedRepository(base, c, logger.New("test"))
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := repo.GetByID(ctx, productA); err != nil {
			t.Fatal(err)
		}
	}
	if calls != 1 {
		t.Fatalf("expected 1 database read, got %d", calls)
	}

	if _, err := repo.ReserveStock(ctx, testOrderID, []StockItem{{ProductID: productA, Quantity: 1}}); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetByID(ctx, productA); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("expected reservation to invalidate the cached product, reads=%d", calls)
	}
}

func TestOrderCancelledHandler_ReleasesAndSkipsGarbage(t *testing.T) {
	var released []string
	h := NewOrderCancelledHandler(&MockRepository{ReleaseFunc: func(_ context.Context, id string) ([]string, error) {
		released = append(released, id)
		return []string{productA}, nil
	}}, logger.New("test"))

	if err := h.Handle(context.Background(), kafkaMsg(`{"order_id":"`+testOrderID+`"}`)); err != nil {
		t.Fatal(err)
	}
	if len(released) != 1 || released[0] != testOrderID {
		t.Fatalf("expected release for %s, got %v", testOrderID, released)
	}
	if err := h.Handle(context.Background(), kafkaMsg(`nope`)); !errors.Is(err, kafka.ErrSkip) {
		t.Fatalf("expected ErrSkip, got %v", err)
	}
}

func kafkaMsg(v string) kafkago.Message {
	return kafkago.Message{Topic: "order.cancelled", Value: []byte(v)}
}

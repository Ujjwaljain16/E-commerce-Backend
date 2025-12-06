package catalog

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	"github.com/lib/pq"
)

func setupMockDB(t *testing.T) (*sql.DB, sqlmock.Sqlmock, Repository) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("Failed to create mock database: %v", err)
	}

	log := logger.New("catalog-test")
	repo := NewPostgresRepository(db, log)

	return db, mock, repo
}

func TestCreate(t *testing.T) {
	db, mock, repo := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	product := &Product{
		Name:        "Test Product",
		Description: "Test Description",
		Price:       99.99,
		SKU:         "TEST-001",
		Stock:       10,
		Images:      []string{"image1.jpg", "image2.jpg"},
		Category:    "Electronics",
	}

	rows := sqlmock.NewRows([]string{"id", "name", "description", "price", "sku", "stock", "images", "category", "created_at", "updated_at"}).
		AddRow("test-id", product.Name, product.Description, product.Price, product.SKU, product.Stock, pq.Array(product.Images), product.Category, time.Now(), time.Now())

	mock.ExpectQuery(`INSERT INTO products`).
		WithArgs(sqlmock.AnyArg(), product.Name, product.Description, product.Price, product.SKU, product.Stock, pq.Array(product.Images), product.Category, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(rows)

	result, err := repo.Create(ctx, product)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if result == nil {
		t.Error("Expected product, got nil")
	}

	if result.Name != product.Name {
		t.Errorf("Expected name %s, got %s", product.Name, result.Name)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestCreate_Error(t *testing.T) {
	db, mock, repo := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	product := &Product{
		Name:        "Test Product",
		Description: "Test Description",
		Price:       99.99,
		SKU:         "TEST-001",
		Stock:       10,
		Images:      []string{"image1.jpg"},
		Category:    "Electronics",
	}

	mock.ExpectQuery(`INSERT INTO products`).
		WithArgs(sqlmock.AnyArg(), product.Name, product.Description, product.Price, product.SKU, product.Stock, pq.Array(product.Images), product.Category, sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnError(sql.ErrConnDone)

	result, err := repo.Create(ctx, product)

	if err == nil {
		t.Error("Expected error, got nil")
	}

	if result != nil {
		t.Errorf("Expected nil, got %v", result)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestGetByID(t *testing.T) {
	db, mock, repo := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	productID := "test-id"

	rows := sqlmock.NewRows([]string{"id", "name", "description", "price", "sku", "stock", "images", "category", "created_at", "updated_at"}).
		AddRow(productID, "Test Product", "Test Description", 99.99, "TEST-001", 10, pq.Array([]string{"image1.jpg"}), "Electronics", time.Now(), time.Now())

	mock.ExpectQuery(`SELECT (.+) FROM products WHERE id`).
		WithArgs(productID).
		WillReturnRows(rows)

	result, err := repo.GetByID(ctx, productID)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if result == nil {
		t.Error("Expected product, got nil")
	}

	if result.ID != productID {
		t.Errorf("Expected ID %s, got %s", productID, result.ID)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestGetByID_NotFound(t *testing.T) {
	db, mock, repo := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	productID := "non-existent"

	mock.ExpectQuery(`SELECT (.+) FROM products WHERE id`).
		WithArgs(productID).
		WillReturnError(sql.ErrNoRows)

	result, err := repo.GetByID(ctx, productID)

	if err == nil {
		t.Error("Expected error, got nil")
	}

	if result != nil {
		t.Errorf("Expected nil, got %v", result)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestGetBySKU(t *testing.T) {
	db, mock, repo := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	sku := "TEST-001"

	rows := sqlmock.NewRows([]string{"id", "name", "description", "price", "sku", "stock", "images", "category", "created_at", "updated_at"}).
		AddRow("test-id", "Test Product", "Test Description", 99.99, sku, 10, pq.Array([]string{"image1.jpg"}), "Electronics", time.Now(), time.Now())

	mock.ExpectQuery(`SELECT (.+) FROM products WHERE sku`).
		WithArgs(sku).
		WillReturnRows(rows)

	result, err := repo.GetBySKU(ctx, sku)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if result == nil {
		t.Error("Expected product, got nil")
	}

	if result.SKU != sku {
		t.Errorf("Expected SKU %s, got %s", sku, result.SKU)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestList(t *testing.T) {
	db, mock, repo := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	page := int32(1)
	pageSize := int32(10)
	category := ""

	countRows := sqlmock.NewRows([]string{"count"}).AddRow(2)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM products`).
		WillReturnRows(countRows)

	rows := sqlmock.NewRows([]string{"id", "name", "description", "price", "sku", "stock", "images", "category", "created_at", "updated_at"}).
		AddRow("id1", "Product 1", "Description 1", 99.99, "SKU-001", 10, pq.Array([]string{"image1.jpg"}), "Electronics", time.Now(), time.Now()).
		AddRow("id2", "Product 2", "Description 2", 149.99, "SKU-002", 20, pq.Array([]string{"image2.jpg"}), "Books", time.Now(), time.Now())

	mock.ExpectQuery(`SELECT (.+) FROM products ORDER BY created_at DESC LIMIT`).
		WithArgs(pageSize, int32(0)).
		WillReturnRows(rows)

	result, total, err := repo.List(ctx, page, pageSize, category)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if len(result) != 2 {
		t.Errorf("Expected 2 products, got %d", len(result))
	}

	if total != 2 {
		t.Errorf("Expected total 2, got %d", total)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestList_WithCategory(t *testing.T) {
	db, mock, repo := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	page := int32(1)
	pageSize := int32(10)
	category := "Electronics"

	countRows := sqlmock.NewRows([]string{"count"}).AddRow(1)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM products WHERE category`).
		WithArgs(category).
		WillReturnRows(countRows)

	rows := sqlmock.NewRows([]string{"id", "name", "description", "price", "sku", "stock", "images", "category", "created_at", "updated_at"}).
		AddRow("id1", "Product 1", "Description 1", 99.99, "SKU-001", 10, pq.Array([]string{"image1.jpg"}), "Electronics", time.Now(), time.Now())

	mock.ExpectQuery(`SELECT (.+) FROM products WHERE category`).
		WithArgs(category, pageSize, int32(0)).
		WillReturnRows(rows)

	result, total, err := repo.List(ctx, page, pageSize, category)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if len(result) != 1 {
		t.Errorf("Expected 1 product, got %d", len(result))
	}

	if total != 1 {
		t.Errorf("Expected total 1, got %d", total)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestUpdate(t *testing.T) {
	db, mock, repo := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	product := &Product{
		ID:          "test-id",
		Name:        "Updated Product",
		Description: "Updated Description",
		Price:       199.99,
		SKU:         "TEST-001",
		Stock:       20,
		Images:      []string{"new-image.jpg"},
		Category:    "Electronics",
	}

	rows := sqlmock.NewRows([]string{"id", "name", "description", "price", "sku", "stock", "images", "category", "created_at", "updated_at"}).
		AddRow(product.ID, product.Name, product.Description, product.Price, product.SKU, product.Stock, pq.Array(product.Images), product.Category, time.Now(), time.Now())

	mock.ExpectQuery(`UPDATE products SET`).
		WithArgs(product.Name, product.Description, product.Price, product.Stock, pq.Array(product.Images), product.Category, sqlmock.AnyArg(), product.ID).
		WillReturnRows(rows)

	result, err := repo.Update(ctx, product)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if result == nil {
		t.Error("Expected product, got nil")
	}

	if result.Name != product.Name {
		t.Errorf("Expected name %s, got %s", product.Name, result.Name)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestUpdate_NotFound(t *testing.T) {
	db, mock, repo := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	product := &Product{
		ID:          "non-existent",
		Name:        "Updated Product",
		Description: "Updated Description",
		Price:       199.99,
		SKU:         "TEST-001",
		Stock:       20,
		Images:      []string{"new-image.jpg"},
		Category:    "Electronics",
	}

	mock.ExpectQuery(`UPDATE products SET`).
		WithArgs(product.Name, product.Description, product.Price, product.Stock, pq.Array(product.Images), product.Category, sqlmock.AnyArg(), product.ID).
		WillReturnError(sql.ErrNoRows)

	result, err := repo.Update(ctx, product)

	if err == nil {
		t.Error("Expected error, got nil")
	}

	if result != nil {
		t.Errorf("Expected nil, got %v", result)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestDelete(t *testing.T) {
	db, mock, repo := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	productID := "test-id"

	mock.ExpectExec(`DELETE FROM products WHERE id`).
		WithArgs(productID).
		WillReturnResult(sqlmock.NewResult(0, 1))

	err := repo.Delete(ctx, productID)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestDelete_NotFound(t *testing.T) {
	db, mock, repo := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	productID := "non-existent"

	mock.ExpectExec(`DELETE FROM products WHERE id`).
		WithArgs(productID).
		WillReturnResult(sqlmock.NewResult(0, 0))

	err := repo.Delete(ctx, productID)

	if err == nil {
		t.Error("Expected error, got nil")
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

func TestSearch(t *testing.T) {
	db, mock, repo := setupMockDB(t)
	defer db.Close()

	ctx := context.Background()
	query := "test"
	page := int32(1)
	pageSize := int32(10)
	searchPattern := "%test%"

	countRows := sqlmock.NewRows([]string{"count"}).AddRow(1)
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM products WHERE`).
		WithArgs(searchPattern).
		WillReturnRows(countRows)

	rows := sqlmock.NewRows([]string{"id", "name", "description", "price", "sku", "stock", "images", "category", "created_at", "updated_at"}).
		AddRow("id1", "Test Product", "Test Description", 99.99, "SKU-001", 10, pq.Array([]string{"image1.jpg"}), "Electronics", time.Now(), time.Now())

	mock.ExpectQuery(`SELECT (.+) FROM products WHERE`).
		WithArgs(searchPattern, pageSize, int32(0)).
		WillReturnRows(rows)

	result, total, err := repo.Search(ctx, query, page, pageSize)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if len(result) != 1 {
		t.Errorf("Expected 1 product, got %d", len(result))
	}

	if total != 1 {
		t.Errorf("Expected total 1, got %d", total)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("Unfulfilled expectations: %v", err)
	}
}

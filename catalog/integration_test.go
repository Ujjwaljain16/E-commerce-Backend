package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"testing"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/catalog/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	_ "github.com/lib/pq"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// setupIntegrationTest creates a PostgreSQL container and returns a configured service
func setupIntegrationTest(t *testing.T) (*Service, func()) {
	t.Helper()
	ctx := context.Background()

	// Create PostgreSQL container
	pgContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("testdb"),
		postgres.WithUsername("testuser"),
		postgres.WithPassword("testpass"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).
				WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		t.Fatalf("Failed to start postgres container: %v", err)
	}

	// Get connection string
	connStr, err := pgContainer.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("Failed to get connection string: %v", err)
	}

	// Connect to database
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}

	// Run migrations
	if err := runMigrations(db); err != nil {
		t.Fatalf("Failed to run migrations: %v", err)
	}

	// Create repository and service
	log := logger.New("catalog-integration-test")
	repo := NewPostgresRepository(db, log)
	service := NewService(repo, log)

	// Cleanup function
	cleanup := func() {
		db.Close()
		if err := pgContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate container: %v", err)
		}
	}

	return service, cleanup
}

// runMigrations applies database schema
func runMigrations(db *sql.DB) error {
	// Create products table
	createTableSQL := `
		CREATE TABLE IF NOT EXISTS products (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			name VARCHAR(255) NOT NULL,
			description TEXT,
			price DECIMAL(10, 2) NOT NULL CHECK (price >= 0),
			sku VARCHAR(100) UNIQUE NOT NULL,
			stock INTEGER NOT NULL DEFAULT 0 CHECK (stock >= 0),
			images TEXT[],
			category VARCHAR(100),
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`
	if _, err := db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("failed to create products table: %w", err)
	}

	// Create indexes
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_products_sku ON products(sku);",
		"CREATE INDEX IF NOT EXISTS idx_products_category ON products(category);",
		"CREATE INDEX IF NOT EXISTS idx_products_name ON products(name);",
	}

	for _, indexSQL := range indexes {
		if _, err := db.Exec(indexSQL); err != nil {
			return fmt.Errorf("failed to create index: %w", err)
		}
	}

	return nil
}

func TestIntegration_CreateProduct(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	req := &pb.CreateProductRequest{
		Name:        "Integration Test Product",
		Description: "Test Description",
		Price:       99.99,
		Sku:         "INT-TEST-001",
		Stock:       50,
		Images:      []string{"image1.jpg", "image2.jpg"},
		Category:    "Electronics",
	}

	resp, err := service.CreateProduct(ctx, req)

	if err != nil {
		t.Fatalf("CreateProduct failed: %v", err)
	}

	if resp.Product == nil {
		t.Fatal("Expected product, got nil")
	}

	if resp.Product.Name != req.Name {
		t.Errorf("Expected name %s, got %s", req.Name, resp.Product.Name)
	}

	if resp.Product.Sku != req.Sku {
		t.Errorf("Expected SKU %s, got %s", req.Sku, resp.Product.Sku)
	}

	if resp.Product.Price != req.Price {
		t.Errorf("Expected price %f, got %f", req.Price, resp.Product.Price)
	}

	if resp.Product.Stock != req.Stock {
		t.Errorf("Expected stock %d, got %d", req.Stock, resp.Product.Stock)
	}

	if resp.Product.Id == "" {
		t.Error("Expected product ID to be set")
	}
}

func TestIntegration_CreateProduct_DuplicateSKU(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create first product
	req := &pb.CreateProductRequest{
		Name:     "Test Product",
		Price:    99.99,
		Sku:      "DUPLICATE-001",
		Stock:    10,
		Category: "Electronics",
	}

	_, err := service.CreateProduct(ctx, req)
	if err != nil {
		t.Fatalf("First CreateProduct failed: %v", err)
	}

	// Try to create duplicate
	_, err = service.CreateProduct(ctx, req)

	if err == nil {
		t.Fatal("Expected error for duplicate SKU, got nil")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.AlreadyExists {
		t.Errorf("Expected AlreadyExists error, got %v", err)
	}
}

func TestIntegration_GetProduct(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create product first
	createReq := &pb.CreateProductRequest{
		Name:     "Test Get Product",
		Price:    149.99,
		Sku:      "GET-TEST-001",
		Stock:    20,
		Category: "Books",
	}

	createResp, err := service.CreateProduct(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateProduct failed: %v", err)
	}

	// Get the product
	getReq := &pb.GetProductRequest{Id: createResp.Product.Id}
	getResp, err := service.GetProduct(ctx, getReq)

	if err != nil {
		t.Fatalf("GetProduct failed: %v", err)
	}

	if getResp.Product.Id != createResp.Product.Id {
		t.Errorf("Expected ID %s, got %s", createResp.Product.Id, getResp.Product.Id)
	}

	if getResp.Product.Name != createReq.Name {
		t.Errorf("Expected name %s, got %s", createReq.Name, getResp.Product.Name)
	}
}

func TestIntegration_ListProducts(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create multiple products
	products := []struct {
		name     string
		sku      string
		category string
	}{
		{"Product 1", "LIST-001", "Electronics"},
		{"Product 2", "LIST-002", "Electronics"},
		{"Product 3", "LIST-003", "Books"},
	}

	for _, p := range products {
		req := &pb.CreateProductRequest{
			Name:     p.name,
			Price:    99.99,
			Sku:      p.sku,
			Stock:    10,
			Category: p.category,
		}
		_, err := service.CreateProduct(ctx, req)
		if err != nil {
			t.Fatalf("Failed to create product %s: %v", p.name, err)
		}
	}

	// List all products
	listReq := &pb.ListProductsRequest{
		Page:     1,
		PageSize: 10,
	}

	listResp, err := service.ListProducts(ctx, listReq)

	if err != nil {
		t.Fatalf("ListProducts failed: %v", err)
	}

	if len(listResp.Products) != 3 {
		t.Errorf("Expected 3 products, got %d", len(listResp.Products))
	}

	if listResp.Total != 3 {
		t.Errorf("Expected total 3, got %d", listResp.Total)
	}
}

func TestIntegration_ListProducts_WithCategory(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create products in different categories
	products := []struct {
		name     string
		sku      string
		category string
	}{
		{"Electronics 1", "CAT-E-001", "Electronics"},
		{"Electronics 2", "CAT-E-002", "Electronics"},
		{"Book 1", "CAT-B-001", "Books"},
	}

	for _, p := range products {
		req := &pb.CreateProductRequest{
			Name:     p.name,
			Price:    99.99,
			Sku:      p.sku,
			Stock:    10,
			Category: p.category,
		}
		_, err := service.CreateProduct(ctx, req)
		if err != nil {
			t.Fatalf("Failed to create product %s: %v", p.name, err)
		}
	}

	// List products in Electronics category
	listReq := &pb.ListProductsRequest{
		Page:     1,
		PageSize: 10,
		Category: "Electronics",
	}

	listResp, err := service.ListProducts(ctx, listReq)

	if err != nil {
		t.Fatalf("ListProducts failed: %v", err)
	}

	if len(listResp.Products) != 2 {
		t.Errorf("Expected 2 Electronics products, got %d", len(listResp.Products))
	}

	if listResp.Total != 2 {
		t.Errorf("Expected total 2, got %d", listResp.Total)
	}
}

func TestIntegration_UpdateProduct(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create product
	createReq := &pb.CreateProductRequest{
		Name:     "Original Product",
		Price:    99.99,
		Sku:      "UPDATE-001",
		Stock:    10,
		Category: "Electronics",
	}

	createResp, err := service.CreateProduct(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateProduct failed: %v", err)
	}

	// Update product
	updateReq := &pb.UpdateProductRequest{
		Id:          createResp.Product.Id,
		Name:        "Updated Product",
		Description: "Updated Description",
		Price:       199.99,
		Stock:       20,
		Images:      []string{"new-image.jpg"},
		Category:    "Books",
	}

	updateResp, err := service.UpdateProduct(ctx, updateReq)

	if err != nil {
		t.Fatalf("UpdateProduct failed: %v", err)
	}

	if updateResp.Product.Name != updateReq.Name {
		t.Errorf("Expected name %s, got %s", updateReq.Name, updateResp.Product.Name)
	}

	if updateResp.Product.Price != updateReq.Price {
		t.Errorf("Expected price %f, got %f", updateReq.Price, updateResp.Product.Price)
	}

	if updateResp.Product.Stock != updateReq.Stock {
		t.Errorf("Expected stock %d, got %d", updateReq.Stock, updateResp.Product.Stock)
	}

	// Verify SKU didn't change
	if updateResp.Product.Sku != createReq.Sku {
		t.Errorf("SKU should not change, expected %s, got %s", createReq.Sku, updateResp.Product.Sku)
	}
}

func TestIntegration_DeleteProduct(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create product
	createReq := &pb.CreateProductRequest{
		Name:     "Product to Delete",
		Price:    99.99,
		Sku:      "DELETE-001",
		Stock:    10,
		Category: "Electronics",
	}

	createResp, err := service.CreateProduct(ctx, createReq)
	if err != nil {
		t.Fatalf("CreateProduct failed: %v", err)
	}

	// Delete product
	deleteReq := &pb.DeleteProductRequest{Id: createResp.Product.Id}
	deleteResp, err := service.DeleteProduct(ctx, deleteReq)

	if err != nil {
		t.Fatalf("DeleteProduct failed: %v", err)
	}

	if !deleteResp.Success {
		t.Error("Expected success to be true")
	}

	// Verify product is deleted
	getReq := &pb.GetProductRequest{Id: createResp.Product.Id}
	_, err = service.GetProduct(ctx, getReq)

	if err == nil {
		t.Error("Expected error when getting deleted product, got nil")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.NotFound {
		t.Errorf("Expected NotFound error, got %v", err)
	}
}

func TestIntegration_SearchProducts(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}

	service, cleanup := setupIntegrationTest(t)
	defer cleanup()

	ctx := context.Background()

	// Create products with searchable names
	products := []struct {
		name string
		sku  string
	}{
		{"Wireless Headphones", "SEARCH-001"},
		{"Bluetooth Speaker", "SEARCH-002"},
		{"Wired Earphones", "SEARCH-003"},
	}

	for _, p := range products {
		req := &pb.CreateProductRequest{
			Name:     p.name,
			Price:    99.99,
			Sku:      p.sku,
			Stock:    10,
			Category: "Electronics",
		}
		_, err := service.CreateProduct(ctx, req)
		if err != nil {
			t.Fatalf("Failed to create product %s: %v", p.name, err)
		}
	}

	// Search for "wireless"
	searchReq := &pb.SearchProductsRequest{
		Query:    "wireless",
		Page:     1,
		PageSize: 10,
	}

	searchResp, err := service.SearchProducts(ctx, searchReq)

	if err != nil {
		t.Fatalf("SearchProducts failed: %v", err)
	}

	if len(searchResp.Products) != 1 {
		t.Errorf("Expected 1 product matching 'wireless', got %d", len(searchResp.Products))
	}

	if searchResp.Total != 1 {
		t.Errorf("Expected total 1, got %d", searchResp.Total)
	}

	if len(searchResp.Products) > 0 && searchResp.Products[0].Name != "Wireless Headphones" {
		t.Errorf("Expected 'Wireless Headphones', got %s", searchResp.Products[0].Name)
	}
}

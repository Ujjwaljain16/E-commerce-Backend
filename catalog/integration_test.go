package catalog

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/catalog/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/database"
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

// runMigrations applies the same embedded migrations the service runs at startup.
func runMigrations(db *sql.DB) error {
	return database.Migrate(context.Background(), db, Migrations, "migrations", "catalog_schema_migrations")
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
		PriceCents:  9999,
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

	if resp.Product.PriceCents != req.PriceCents {
		t.Errorf("Expected price %d, got %d", req.PriceCents, resp.Product.PriceCents)
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
		Name:       "Test Product",
		PriceCents: 9999,
		Sku:        "DUPLICATE-001",
		Stock:      10,
		Category:   "Electronics",
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
		Name:       "Test Get Product",
		PriceCents: 14999,
		Sku:        "GET-TEST-001",
		Stock:      20,
		Category:   "Books",
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
			Name:       p.name,
			PriceCents: 9999,
			Sku:        p.sku,
			Stock:      10,
			Category:   p.category,
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
			Name:       p.name,
			PriceCents: 9999,
			Sku:        p.sku,
			Stock:      10,
			Category:   p.category,
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
		Name:       "Original Product",
		PriceCents: 9999,
		Sku:        "UPDATE-001",
		Stock:      10,
		Category:   "Electronics",
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
		PriceCents:  19999,
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

	if updateResp.Product.PriceCents != updateReq.PriceCents {
		t.Errorf("Expected price %d, got %d", updateReq.PriceCents, updateResp.Product.PriceCents)
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
		Name:       "Product to Delete",
		PriceCents: 9999,
		Sku:        "DELETE-001",
		Stock:      10,
		Category:   "Electronics",
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
			Name:       p.name,
			PriceCents: 9999,
			Sku:        p.sku,
			Stock:      10,
			Category:   "Electronics",
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

func TestIntegration_ReservationExpiryAndLateCommit(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test")
	}
	service, cleanup := setupIntegrationTest(t)
	defer cleanup()
	ctx := context.Background()
	repo := service.repo.(*postgresRepository)

	created, err := service.CreateProduct(ctx, &pb.CreateProductRequest{Name: "Kettle", PriceCents: 3000, Sku: "EXP-1", Stock: 5})
	if err != nil {
		t.Fatal(err)
	}
	productID := created.Product.Id
	stock := func() int32 {
		p, err := service.GetProduct(ctx, &pb.GetProductRequest{Id: productID})
		if err != nil {
			t.Fatal(err)
		}
		return p.Product.Stock
	}

	orphan, lateOrder := "6b1f7d0e-0000-4000-8000-000000000001", "6b1f7d0e-0000-4000-8000-000000000002"
	for _, id := range []string{orphan, lateOrder} {
		if _, err := service.ReserveStock(ctx, &pb.ReserveStockRequest{OrderId: id, Items: []*pb.StockItem{{ProductId: productID, Quantity: 2}}}); err != nil {
			t.Fatal(err)
		}
	}
	if got := stock(); got != 1 {
		t.Fatalf("after two reservations want stock 1, got %d", got)
	}

	if _, err := repo.db.ExecContext(ctx, `UPDATE stock_reservations SET created_at = NOW() - INTERVAL '1 hour'`); err != nil {
		t.Fatal(err)
	}
	if n := NewReservationSweeper(repo, logger.New("test"), 15*time.Minute).SweepOnce(ctx); n != 2 {
		t.Fatalf("expected 2 reservations expired, got %d", n)
	}
	if got := stock(); got != 5 {
		t.Fatalf("expired reservations must return stock, got %d", got)
	}

	// The order for lateOrder did exist; its event just arrived late. It should win the stock back.
	if _, err := repo.CommitReservation(ctx, lateOrder); err != nil {
		t.Fatal(err)
	}
	if got := stock(); got != 3 {
		t.Fatalf("late commit should re-reserve, want 3, got %d", got)
	}
	// Cancelling it afterwards releases it like any committed reservation.
	if ids, err := repo.ReleaseStock(ctx, lateOrder); err != nil || len(ids) != 1 {
		t.Fatalf("release committed reservation = %v, %v", ids, err)
	}
	if got := stock(); got != 5 {
		t.Fatalf("want stock 5 after release, got %d", got)
	}
	// A second sweep finds nothing.
	if n := NewReservationSweeper(repo, logger.New("test"), 15*time.Minute).SweepOnce(ctx); n != 0 {
		t.Fatalf("second sweep released %d", n)
	}
}

package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/catalog/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MockRepository is a mock implementation of Repository for testing
type MockRepository struct {
	CreateFunc   func(ctx context.Context, product *Product) (*Product, error)
	GetByIDFunc  func(ctx context.Context, id string) (*Product, error)
	GetBySKUFunc func(ctx context.Context, sku string) (*Product, error)
	ListFunc     func(ctx context.Context, page, pageSize int32, category string) ([]*Product, int32, error)
	UpdateFunc   func(ctx context.Context, product *Product) (*Product, error)
	DeleteFunc   func(ctx context.Context, id string) error
	SearchFunc   func(ctx context.Context, query string, page, pageSize int32) ([]*Product, int32, error)
	CloseFunc    func() error
}

func (m *MockRepository) Create(ctx context.Context, product *Product) (*Product, error) {
	if m.CreateFunc != nil {
		return m.CreateFunc(ctx, product)
	}
	return nil, errors.New("not implemented")
}

func (m *MockRepository) GetByID(ctx context.Context, id string) (*Product, error) {
	if m.GetByIDFunc != nil {
		return m.GetByIDFunc(ctx, id)
	}
	return nil, errors.New("not implemented")
}

func (m *MockRepository) GetBySKU(ctx context.Context, sku string) (*Product, error) {
	if m.GetBySKUFunc != nil {
		return m.GetBySKUFunc(ctx, sku)
	}
	return nil, errors.New("not implemented")
}

func (m *MockRepository) List(ctx context.Context, page, pageSize int32, category string) ([]*Product, int32, error) {
	if m.ListFunc != nil {
		return m.ListFunc(ctx, page, pageSize, category)
	}
	return nil, 0, errors.New("not implemented")
}

func (m *MockRepository) Update(ctx context.Context, product *Product) (*Product, error) {
	if m.UpdateFunc != nil {
		return m.UpdateFunc(ctx, product)
	}
	return nil, errors.New("not implemented")
}

func (m *MockRepository) Delete(ctx context.Context, id string) error {
	if m.DeleteFunc != nil {
		return m.DeleteFunc(ctx, id)
	}
	return errors.New("not implemented")
}

func (m *MockRepository) Search(ctx context.Context, query string, page, pageSize int32) ([]*Product, int32, error) {
	if m.SearchFunc != nil {
		return m.SearchFunc(ctx, query, page, pageSize)
	}
	return nil, 0, errors.New("not implemented")
}

func (m *MockRepository) Close() error {
	if m.CloseFunc != nil {
		return m.CloseFunc()
	}
	return nil
}

func setupService(repo Repository) *Service {
	log := logger.New("catalog-test")
	return NewService(repo, log)
}

func TestCreateProduct_Success(t *testing.T) {
	mockRepo := &MockRepository{
		GetBySKUFunc: func(ctx context.Context, sku string) (*Product, error) {
			return nil, errors.New("not found")
		},
		CreateFunc: func(ctx context.Context, product *Product) (*Product, error) {
			product.ID = "test-id"
			product.CreatedAt = time.Now()
			product.UpdatedAt = time.Now()
			return product, nil
		},
	}

	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.CreateProductRequest{
		Name:        "Test Product",
		Description: "Test Description",
		Price:       99.99,
		Sku:         "TEST-001",
		Stock:       10,
		Images:      []string{"image1.jpg"},
		Category:    "Electronics",
	}

	resp, err := service.CreateProduct(ctx, req)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if resp == nil {
		t.Fatal("Expected response, got nil")
	}

	if resp.Product.Name != req.Name {
		t.Errorf("Expected name %s, got %s", req.Name, resp.Product.Name)
	}

	if resp.Product.Sku != req.Sku {
		t.Errorf("Expected SKU %s, got %s", req.Sku, resp.Product.Sku)
	}
}

func TestCreateProduct_MissingName(t *testing.T) {
	mockRepo := &MockRepository{}
	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.CreateProductRequest{
		Name:  "",
		Price: 99.99,
		Sku:   "TEST-001",
		Stock: 10,
	}

	resp, err := service.CreateProduct(ctx, req)

	if err == nil {
		t.Error("Expected error, got nil")
	}

	if resp != nil {
		t.Errorf("Expected nil response, got %v", resp)
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.InvalidArgument {
		t.Errorf("Expected InvalidArgument error, got %v", err)
	}
}

func TestCreateProduct_MissingSKU(t *testing.T) {
	mockRepo := &MockRepository{}
	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.CreateProductRequest{
		Name:  "Test Product",
		Price: 99.99,
		Sku:   "",
		Stock: 10,
	}

	resp, err := service.CreateProduct(ctx, req)

	if err == nil {
		t.Error("Expected error, got nil")
	}

	if resp != nil {
		t.Errorf("Expected nil response, got %v", resp)
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.InvalidArgument {
		t.Errorf("Expected InvalidArgument error, got %v", err)
	}
}

func TestCreateProduct_InvalidPrice(t *testing.T) {
	mockRepo := &MockRepository{}
	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.CreateProductRequest{
		Name:  "Test Product",
		Price: -10.0,
		Sku:   "TEST-001",
		Stock: 10,
	}

	_, err := service.CreateProduct(ctx, req)

	if err == nil {
		t.Error("Expected error, got nil")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.InvalidArgument {
		t.Errorf("Expected InvalidArgument error, got %v", err)
	}
}

func TestCreateProduct_NegativeStock(t *testing.T) {
	mockRepo := &MockRepository{}
	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.CreateProductRequest{
		Name:  "Test Product",
		Price: 99.99,
		Sku:   "TEST-001",
		Stock: -5,
	}

	_, err := service.CreateProduct(ctx, req)

	if err == nil {
		t.Error("Expected error, got nil")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.InvalidArgument {
		t.Errorf("Expected InvalidArgument error, got %v", err)
	}
}

func TestCreateProduct_DuplicateSKU(t *testing.T) {
	mockRepo := &MockRepository{
		GetBySKUFunc: func(ctx context.Context, sku string) (*Product, error) {
			return &Product{ID: "existing-id", SKU: sku}, nil
		},
	}

	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.CreateProductRequest{
		Name:  "Test Product",
		Price: 99.99,
		Sku:   "TEST-001",
		Stock: 10,
	}

	_, err := service.CreateProduct(ctx, req)

	if err == nil {
		t.Error("Expected error, got nil")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.AlreadyExists {
		t.Errorf("Expected AlreadyExists error, got %v", err)
	}
}

func TestGetProduct_Success(t *testing.T) {
	mockRepo := &MockRepository{
		GetByIDFunc: func(ctx context.Context, id string) (*Product, error) {
			return &Product{
				ID:          id,
				Name:        "Test Product",
				Description: "Test Description",
				Price:       99.99,
				SKU:         "TEST-001",
				Stock:       10,
				Images:      []string{"image1.jpg"},
				Category:    "Electronics",
				CreatedAt:   time.Now(),
				UpdatedAt:   time.Now(),
			}, nil
		},
	}

	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.GetProductRequest{Id: "test-id"}
	resp, err := service.GetProduct(ctx, req)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if resp == nil {
		t.Fatal("Expected response, got nil")
	}

	if resp.Product.Id != "test-id" {
		t.Errorf("Expected ID test-id, got %s", resp.Product.Id)
	}
}

func TestGetProduct_MissingID(t *testing.T) {
	mockRepo := &MockRepository{}
	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.GetProductRequest{Id: ""}
	_, err := service.GetProduct(ctx, req)

	if err == nil {
		t.Error("Expected error, got nil")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.InvalidArgument {
		t.Errorf("Expected InvalidArgument error, got %v", err)
	}
}

func TestGetProduct_NotFound(t *testing.T) {
	mockRepo := &MockRepository{
		GetByIDFunc: func(ctx context.Context, id string) (*Product, error) {
			return nil, errors.New("not found")
		},
	}

	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.GetProductRequest{Id: "non-existent"}
	_, err := service.GetProduct(ctx, req)

	if err == nil {
		t.Error("Expected error, got nil")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.NotFound {
		t.Errorf("Expected NotFound error, got %v", err)
	}
}

func TestListProducts_Success(t *testing.T) {
	mockRepo := &MockRepository{
		ListFunc: func(ctx context.Context, page, pageSize int32, category string) ([]*Product, int32, error) {
			return []*Product{
				{
					ID:        "id1",
					Name:      "Product 1",
					Price:     99.99,
					SKU:       "SKU-001",
					Stock:     10,
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				},
				{
					ID:        "id2",
					Name:      "Product 2",
					Price:     149.99,
					SKU:       "SKU-002",
					Stock:     20,
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				},
			}, 2, nil
		},
	}

	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.ListProductsRequest{
		Page:     1,
		PageSize: 10,
	}

	resp, err := service.ListProducts(ctx, req)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if resp == nil {
		t.Fatal("Expected response, got nil")
	}

	if len(resp.Products) != 2 {
		t.Errorf("Expected 2 products, got %d", len(resp.Products))
	}

	if resp.Total != 2 {
		t.Errorf("Expected total 2, got %d", resp.Total)
	}
}

func TestListProducts_WithCategory(t *testing.T) {
	mockRepo := &MockRepository{
		ListFunc: func(ctx context.Context, page, pageSize int32, category string) ([]*Product, int32, error) {
			if category != "Electronics" {
				t.Errorf("Expected category Electronics, got %s", category)
			}
			return []*Product{}, 0, nil
		},
	}

	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.ListProductsRequest{
		Page:     1,
		PageSize: 10,
		Category: "Electronics",
	}

	_, err := service.ListProducts(ctx, req)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}
}

func TestUpdateProduct_Success(t *testing.T) {
	mockRepo := &MockRepository{
		GetByIDFunc: func(ctx context.Context, id string) (*Product, error) {
			return &Product{
				ID:        id,
				SKU:       "TEST-001",
				CreatedAt: time.Now(),
			}, nil
		},
		UpdateFunc: func(ctx context.Context, product *Product) (*Product, error) {
			product.UpdatedAt = time.Now()
			return product, nil
		},
	}

	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.UpdateProductRequest{
		Id:          "test-id",
		Name:        "Updated Product",
		Description: "Updated Description",
		Price:       199.99,
		Stock:       20,
		Images:      []string{"new-image.jpg"},
		Category:    "Electronics",
	}

	resp, err := service.UpdateProduct(ctx, req)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if resp == nil {
		t.Fatal("Expected response, got nil")
	}

	if resp.Product.Name != req.Name {
		t.Errorf("Expected name %s, got %s", req.Name, resp.Product.Name)
	}
}

func TestUpdateProduct_MissingID(t *testing.T) {
	mockRepo := &MockRepository{}
	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.UpdateProductRequest{
		Id:    "",
		Name:  "Updated Product",
		Price: 199.99,
		Stock: 20,
	}

	_, err := service.UpdateProduct(ctx, req)

	if err == nil {
		t.Error("Expected error, got nil")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.InvalidArgument {
		t.Errorf("Expected InvalidArgument error, got %v", err)
	}
}

func TestUpdateProduct_NotFound(t *testing.T) {
	mockRepo := &MockRepository{
		GetByIDFunc: func(ctx context.Context, id string) (*Product, error) {
			return nil, errors.New("not found")
		},
	}

	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.UpdateProductRequest{
		Id:    "non-existent",
		Name:  "Updated Product",
		Price: 199.99,
		Stock: 20,
	}

	_, err := service.UpdateProduct(ctx, req)

	if err == nil {
		t.Error("Expected error, got nil")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.NotFound {
		t.Errorf("Expected NotFound error, got %v", err)
	}
}

func TestDeleteProduct_Success(t *testing.T) {
	mockRepo := &MockRepository{
		DeleteFunc: func(ctx context.Context, id string) error {
			return nil
		},
	}

	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.DeleteProductRequest{Id: "test-id"}
	resp, err := service.DeleteProduct(ctx, req)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if resp == nil {
		t.Fatal("Expected response, got nil")
	}

	if !resp.Success {
		t.Error("Expected success to be true")
	}
}

func TestDeleteProduct_MissingID(t *testing.T) {
	mockRepo := &MockRepository{}
	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.DeleteProductRequest{Id: ""}
	_, err := service.DeleteProduct(ctx, req)

	if err == nil {
		t.Error("Expected error, got nil")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.InvalidArgument {
		t.Errorf("Expected InvalidArgument error, got %v", err)
	}
}

func TestDeleteProduct_NotFound(t *testing.T) {
	mockRepo := &MockRepository{
		DeleteFunc: func(ctx context.Context, id string) error {
			return errors.New("not found")
		},
	}

	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.DeleteProductRequest{Id: "non-existent"}
	_, err := service.DeleteProduct(ctx, req)

	if err == nil {
		t.Error("Expected error, got nil")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.NotFound {
		t.Errorf("Expected NotFound error, got %v", err)
	}
}

func TestSearchProducts_Success(t *testing.T) {
	mockRepo := &MockRepository{
		SearchFunc: func(ctx context.Context, query string, page, pageSize int32) ([]*Product, int32, error) {
			return []*Product{
				{
					ID:        "id1",
					Name:      "Test Product",
					Price:     99.99,
					SKU:       "SKU-001",
					Stock:     10,
					CreatedAt: time.Now(),
					UpdatedAt: time.Now(),
				},
			}, 1, nil
		},
	}

	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.SearchProductsRequest{
		Query:    "test",
		Page:     1,
		PageSize: 10,
	}

	resp, err := service.SearchProducts(ctx, req)

	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if resp == nil {
		t.Fatal("Expected response, got nil")
	}

	if len(resp.Products) != 1 {
		t.Errorf("Expected 1 product, got %d", len(resp.Products))
	}

	if resp.Total != 1 {
		t.Errorf("Expected total 1, got %d", resp.Total)
	}
}

func TestSearchProducts_MissingQuery(t *testing.T) {
	mockRepo := &MockRepository{}
	service := setupService(mockRepo)
	ctx := context.Background()

	req := &pb.SearchProductsRequest{
		Query:    "",
		Page:     1,
		PageSize: 10,
	}

	_, err := service.SearchProducts(ctx, req)

	if err == nil {
		t.Error("Expected error, got nil")
	}

	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.InvalidArgument {
		t.Errorf("Expected InvalidArgument error, got %v", err)
	}
}

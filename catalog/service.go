package catalog

import (
	"context"

	"github.com/Ujjwaljain16/E-commerce-Backend/catalog/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Service implements the CatalogService gRPC interface
type Service struct {
	pb.UnimplementedCatalogServiceServer
	repo Repository
	log  *logger.Logger
}

// NewService creates a new catalog service
func NewService(repo Repository, log *logger.Logger) *Service {
	return &Service{
		repo: repo,
		log:  log,
	}
}

// CreateProduct creates a new product in the catalog
func (s *Service) CreateProduct(ctx context.Context, req *pb.CreateProductRequest) (*pb.CreateProductResponse, error) {
	// Validate input
	if req.Name == "" {
		s.log.Warn(ctx, "Create product failed: name is required", nil)
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if req.Sku == "" {
		s.log.Warn(ctx, "Create product failed: SKU is required", nil)
		return nil, status.Error(codes.InvalidArgument, "sku is required")
	}
	if req.Price <= 0 {
		s.log.Warn(ctx, "Create product failed: price must be positive", nil)
		return nil, status.Error(codes.InvalidArgument, "price must be positive")
	}
	if req.Stock < 0 {
		s.log.Warn(ctx, "Create product failed: stock cannot be negative", nil)
		return nil, status.Error(codes.InvalidArgument, "stock cannot be negative")
	}

	// Check if SKU already exists
	existing, err := s.repo.GetBySKU(ctx, req.Sku)
	if err == nil && existing != nil {
		s.log.Warn(ctx, "Create product failed: SKU already exists", map[string]interface{}{"sku": req.Sku})
		return nil, status.Error(codes.AlreadyExists, "product with this SKU already exists")
	}

	// Create product
	product := &Product{
		Name:        req.Name,
		Description: req.Description,
		Price:       req.Price,
		SKU:         req.Sku,
		Stock:       req.Stock,
		Images:      req.Images,
		Category:    req.Category,
	}

	created, err := s.repo.Create(ctx, product)
	if err != nil {
		s.log.Error(ctx, "Failed to create product", map[string]interface{}{"error": err.Error()})
		return nil, status.Error(codes.Internal, "failed to create product")
	}

	s.log.Info(ctx, "Product created successfully", map[string]interface{}{"product_id": created.ID, "sku": created.SKU})

	return &pb.CreateProductResponse{
		Product: toProtoProduct(created),
	}, nil
}

// GetProduct retrieves a product by ID
func (s *Service) GetProduct(ctx context.Context, req *pb.GetProductRequest) (*pb.GetProductResponse, error) {
	if req.Id == "" {
		s.log.Warn(ctx, "Get product failed: ID is required", nil)
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	product, err := s.repo.GetByID(ctx, req.Id)
	if err != nil {
		s.log.Warn(ctx, "Product not found", map[string]interface{}{"product_id": req.Id})
		return nil, status.Error(codes.NotFound, "product not found")
	}

	return &pb.GetProductResponse{
		Product: toProtoProduct(product),
	}, nil
}

// ListProducts retrieves a paginated list of products
func (s *Service) ListProducts(ctx context.Context, req *pb.ListProductsRequest) (*pb.ListProductsResponse, error) {
	page := req.Page
	if page < 1 {
		page = 1
	}

	pageSize := req.PageSize
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}

	products, total, err := s.repo.List(ctx, page, pageSize, req.Category)
	if err != nil {
		s.log.Error(ctx, "Failed to list products", map[string]interface{}{"error": err.Error()})
		return nil, status.Error(codes.Internal, "failed to list products")
	}

	protoProducts := make([]*pb.Product, len(products))
	for i, p := range products {
		protoProducts[i] = toProtoProduct(p)
	}

	s.log.Info(ctx, "Products listed successfully", map[string]interface{}{"count": len(products), "total": total})

	return &pb.ListProductsResponse{
		Products: protoProducts,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// UpdateProduct updates an existing product
func (s *Service) UpdateProduct(ctx context.Context, req *pb.UpdateProductRequest) (*pb.UpdateProductResponse, error) {
	if req.Id == "" {
		s.log.Warn(ctx, "Update product failed: ID is required", nil)
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	// Validate input
	if req.Name == "" {
		s.log.Warn(ctx, "Update product failed: name is required", nil)
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}
	if req.Price <= 0 {
		s.log.Warn(ctx, "Update product failed: price must be positive", nil)
		return nil, status.Error(codes.InvalidArgument, "price must be positive")
	}
	if req.Stock < 0 {
		s.log.Warn(ctx, "Update product failed: stock cannot be negative", nil)
		return nil, status.Error(codes.InvalidArgument, "stock cannot be negative")
	}

	// Check if product exists
	existing, err := s.repo.GetByID(ctx, req.Id)
	if err != nil {
		s.log.Warn(ctx, "Product not found for update", map[string]interface{}{"product_id": req.Id})
		return nil, status.Error(codes.NotFound, "product not found")
	}

	// Update product
	product := &Product{
		ID:          existing.ID,
		Name:        req.Name,
		Description: req.Description,
		Price:       req.Price,
		SKU:         existing.SKU, // SKU cannot be updated
		Stock:       req.Stock,
		Images:      req.Images,
		Category:    req.Category,
	}

	updated, err := s.repo.Update(ctx, product)
	if err != nil {
		s.log.Error(ctx, "Failed to update product", map[string]interface{}{"error": err.Error(), "product_id": req.Id})
		return nil, status.Error(codes.Internal, "failed to update product")
	}

	s.log.Info(ctx, "Product updated successfully", map[string]interface{}{"product_id": updated.ID})

	return &pb.UpdateProductResponse{
		Product: toProtoProduct(updated),
	}, nil
}

// DeleteProduct deletes a product
func (s *Service) DeleteProduct(ctx context.Context, req *pb.DeleteProductRequest) (*pb.DeleteProductResponse, error) {
	if req.Id == "" {
		s.log.Warn(ctx, "Delete product failed: ID is required", nil)
		return nil, status.Error(codes.InvalidArgument, "id is required")
	}

	err := s.repo.Delete(ctx, req.Id)
	if err != nil {
		s.log.Warn(ctx, "Failed to delete product", map[string]interface{}{"error": err.Error(), "product_id": req.Id})
		return nil, status.Error(codes.NotFound, "product not found")
	}

	s.log.Info(ctx, "Product deleted successfully", map[string]interface{}{"product_id": req.Id})

	return &pb.DeleteProductResponse{
		Success: true,
		Message: "Product deleted successfully",
	}, nil
}

// SearchProducts searches for products by name or description
func (s *Service) SearchProducts(ctx context.Context, req *pb.SearchProductsRequest) (*pb.SearchProductsResponse, error) {
	if req.Query == "" {
		s.log.Warn(ctx, "Search products failed: query is required", nil)
		return nil, status.Error(codes.InvalidArgument, "query is required")
	}

	page := req.Page
	if page < 1 {
		page = 1
	}

	pageSize := req.PageSize
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}

	products, total, err := s.repo.Search(ctx, req.Query, page, pageSize)
	if err != nil {
		s.log.Error(ctx, "Failed to search products", map[string]interface{}{"error": err.Error(), "query": req.Query})
		return nil, status.Error(codes.Internal, "failed to search products")
	}

	protoProducts := make([]*pb.Product, len(products))
	for i, p := range products {
		protoProducts[i] = toProtoProduct(p)
	}

	s.log.Info(ctx, "Products searched successfully", map[string]interface{}{"query": req.Query, "count": len(products), "total": total})

	return &pb.SearchProductsResponse{
		Products: protoProducts,
		Total:    total,
	}, nil
}

// toProtoProduct converts a domain Product to a protobuf Product
func toProtoProduct(p *Product) *pb.Product {
	if p == nil {
		return nil
	}

	return &pb.Product{
		Id:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		Price:       p.Price,
		Sku:         p.SKU,
		Stock:       p.Stock,
		Images:      p.Images,
		Category:    p.Category,
		CreatedAt:   timestamppb.New(p.CreatedAt),
		UpdatedAt:   timestamppb.New(p.UpdatedAt),
	}
}

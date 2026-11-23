package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Product represents a product in the catalog
type Product struct {
	ID          string
	Name        string
	Description string
	PriceCents  int64
	SKU         string
	Stock       int32
	Images      []string
	Category    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Repository handles product data persistence
type Repository interface {
	Create(ctx context.Context, product *Product) (*Product, error)
	GetByID(ctx context.Context, id string) (*Product, error)
	GetBySKU(ctx context.Context, sku string) (*Product, error)
	List(ctx context.Context, page, pageSize int32, category string) ([]*Product, int32, error)
	Update(ctx context.Context, product *Product) (*Product, error)
	Delete(ctx context.Context, id string) error
	Search(ctx context.Context, query string, page, pageSize int32) ([]*Product, int32, error)
	ReserveStock(ctx context.Context, orderID string, items []StockItem) ([]ReservedItem, error)
	// ReleaseStock returns the IDs of products whose stock was put back (empty if nothing was reserved).
	ReleaseStock(ctx context.Context, orderID string) ([]string, error)
	// CommitReservation marks a reservation as belonging to a created order (restoring it if it had expired).
	CommitReservation(ctx context.Context, orderID string) ([]string, error)
	ExpiredReservations(ctx context.Context, ttl time.Duration, limit int) ([]string, error)
	ExpireReservation(ctx context.Context, orderID string) ([]string, error)
	Close() error
}

type postgresRepository struct {
	db  *sql.DB
	log *logger.Logger
}

// NewPostgresRepository creates a new PostgreSQL repository
func NewPostgresRepository(db *sql.DB, log *logger.Logger) Repository {
	return &postgresRepository{
		db:  db,
		log: log,
	}
}

// Create creates a new product
func (r *postgresRepository) Create(ctx context.Context, product *Product) (*Product, error) {
	product.ID = uuid.New().String()
	product.CreatedAt = time.Now()
	product.UpdatedAt = time.Now()

	query := `
		INSERT INTO products (id, name, description, price_cents, sku, stock, images, category, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id, name, description, price_cents, sku, stock, images, category, created_at, updated_at
	`

	var images pq.StringArray
	err := r.db.QueryRowContext(
		ctx,
		query,
		product.ID,
		product.Name,
		product.Description,
		product.PriceCents,
		product.SKU,
		product.Stock,
		pq.Array(product.Images),
		product.Category,
		product.CreatedAt,
		product.UpdatedAt,
	).Scan(
		&product.ID,
		&product.Name,
		&product.Description,
		&product.PriceCents,
		&product.SKU,
		&product.Stock,
		&images,
		&product.Category,
		&product.CreatedAt,
		&product.UpdatedAt,
	)

	if err != nil {
		r.log.Error(ctx, "Failed to create product", map[string]interface{}{"error": err.Error()})
		return nil, fmt.Errorf("failed to create product: %w", err)
	}

	product.Images = images
	r.log.Info(ctx, "Product created successfully", map[string]interface{}{"product_id": product.ID, "sku": product.SKU})
	return product, nil
}

// GetByID retrieves a product by ID
func (r *postgresRepository) GetByID(ctx context.Context, id string) (*Product, error) {
	query := `
		SELECT id, name, description, price_cents, sku, stock, images, category, created_at, updated_at
		FROM products
		WHERE id = $1
	`

	product := &Product{}
	var images pq.StringArray

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&product.ID,
		&product.Name,
		&product.Description,
		&product.PriceCents,
		&product.SKU,
		&product.Stock,
		&images,
		&product.Category,
		&product.CreatedAt,
		&product.UpdatedAt,
	)

	if err == sql.ErrNoRows || isInvalidID(err) {
		r.log.Warn(ctx, "Product not found", map[string]interface{}{"product_id": id})
		return nil, ErrProductNotFound
	}

	if err != nil {
		r.log.Error(ctx, "Failed to get product", map[string]interface{}{"error": err.Error(), "product_id": id})
		return nil, fmt.Errorf("failed to get product: %w", err)
	}

	product.Images = images
	return product, nil
}

// GetBySKU retrieves a product by SKU
func (r *postgresRepository) GetBySKU(ctx context.Context, sku string) (*Product, error) {
	query := `
		SELECT id, name, description, price_cents, sku, stock, images, category, created_at, updated_at
		FROM products
		WHERE sku = $1
	`

	product := &Product{}
	var images pq.StringArray

	err := r.db.QueryRowContext(ctx, query, sku).Scan(
		&product.ID,
		&product.Name,
		&product.Description,
		&product.PriceCents,
		&product.SKU,
		&product.Stock,
		&images,
		&product.Category,
		&product.CreatedAt,
		&product.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		r.log.Warn(ctx, "Product not found", map[string]interface{}{"sku": sku})
		return nil, fmt.Errorf("product not found")
	}

	if err != nil {
		r.log.Error(ctx, "Failed to get product by SKU", map[string]interface{}{"error": err.Error(), "sku": sku})
		return nil, fmt.Errorf("failed to get product: %w", err)
	}

	product.Images = images
	return product, nil
}

// List retrieves products with pagination and optional category filter
func (r *postgresRepository) List(ctx context.Context, page, pageSize int32, category string) ([]*Product, int32, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}

	offset := (page - 1) * pageSize

	// Build query with optional category filter
	var query string
	var countQuery string
	var args []interface{}

	if category != "" {
		query = `
			SELECT id, name, description, price_cents, sku, stock, images, category, created_at, updated_at
			FROM products
			WHERE category = $1
			ORDER BY created_at DESC
			LIMIT $2 OFFSET $3
		`
		countQuery = "SELECT COUNT(*) FROM products WHERE category = $1"
		args = []interface{}{category, pageSize, offset}
	} else {
		query = `
			SELECT id, name, description, price_cents, sku, stock, images, category, created_at, updated_at
			FROM products
			ORDER BY created_at DESC
			LIMIT $1 OFFSET $2
		`
		countQuery = "SELECT COUNT(*) FROM products"
		args = []interface{}{pageSize, offset}
	}

	// Get total count
	var total int32
	var countArgs []interface{}
	if category != "" {
		countArgs = []interface{}{category}
	}
	err := r.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&total)
	if err != nil {
		r.log.Error(ctx, "Failed to count products", map[string]interface{}{"error": err.Error()})
		return nil, 0, fmt.Errorf("failed to count products: %w", err)
	}

	// Get products
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		r.log.Error(ctx, "Failed to list products", map[string]interface{}{"error": err.Error()})
		return nil, 0, fmt.Errorf("failed to list products: %w", err)
	}
	defer rows.Close()

	products := []*Product{}
	for rows.Next() {
		product := &Product{}
		var images pq.StringArray

		err := rows.Scan(
			&product.ID,
			&product.Name,
			&product.Description,
			&product.PriceCents,
			&product.SKU,
			&product.Stock,
			&images,
			&product.Category,
			&product.CreatedAt,
			&product.UpdatedAt,
		)
		if err != nil {
			r.log.Error(ctx, "Failed to scan product", map[string]interface{}{"error": err.Error()})
			return nil, 0, fmt.Errorf("failed to scan product: %w", err)
		}

		product.Images = images
		products = append(products, product)
	}

	if err = rows.Err(); err != nil {
		r.log.Error(ctx, "Error iterating products", map[string]interface{}{"error": err.Error()})
		return nil, 0, fmt.Errorf("error iterating products: %w", err)
	}

	r.log.Info(ctx, "Products listed successfully", map[string]interface{}{"count": len(products), "total": total})
	return products, total, nil
}

// Update updates an existing product
func (r *postgresRepository) Update(ctx context.Context, product *Product) (*Product, error) {
	query := `
		UPDATE products
		SET name = $1, description = $2, price_cents = $3, stock = $4, images = $5, category = $6, updated_at = $7
		WHERE id = $8
		RETURNING id, name, description, price_cents, sku, stock, images, category, created_at, updated_at
	`

	product.UpdatedAt = time.Now()
	var images pq.StringArray

	err := r.db.QueryRowContext(
		ctx,
		query,
		product.Name,
		product.Description,
		product.PriceCents,
		product.Stock,
		pq.Array(product.Images),
		product.Category,
		product.UpdatedAt,
		product.ID,
	).Scan(
		&product.ID,
		&product.Name,
		&product.Description,
		&product.PriceCents,
		&product.SKU,
		&product.Stock,
		&images,
		&product.Category,
		&product.CreatedAt,
		&product.UpdatedAt,
	)

	if err == sql.ErrNoRows {
		r.log.Warn(ctx, "Product not found for update", map[string]interface{}{"product_id": product.ID})
		return nil, fmt.Errorf("product not found")
	}

	if err != nil {
		r.log.Error(ctx, "Failed to update product", map[string]interface{}{"error": err.Error(), "product_id": product.ID})
		return nil, fmt.Errorf("failed to update product: %w", err)
	}

	product.Images = images
	r.log.Info(ctx, "Product updated successfully", map[string]interface{}{"product_id": product.ID})
	return product, nil
}

// Delete deletes a product
func (r *postgresRepository) Delete(ctx context.Context, id string) error {
	query := "DELETE FROM products WHERE id = $1"

	result, err := r.db.ExecContext(ctx, query, id)
	if isInvalidID(err) {
		return ErrProductNotFound
	}
	if err != nil {
		r.log.Error(ctx, "Failed to delete product", map[string]interface{}{"error": err.Error(), "product_id": id})
		return fmt.Errorf("failed to delete product: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		r.log.Error(ctx, "Failed to get rows affected", map[string]interface{}{"error": err.Error()})
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rows == 0 {
		r.log.Warn(ctx, "Product not found for deletion", map[string]interface{}{"product_id": id})
		return ErrProductNotFound
	}

	r.log.Info(ctx, "Product deleted successfully", map[string]interface{}{"product_id": id})
	return nil
}

// Search searches for products by name or description
func (r *postgresRepository) Search(ctx context.Context, query string, page, pageSize int32) ([]*Product, int32, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}

	offset := (page - 1) * pageSize
	query = strings.TrimSpace(query)

	// Full-text search on the weighted search_vector column (name ranks above description).
	countQuery := `
		SELECT COUNT(*)
		FROM products
		WHERE search_vector @@ websearch_to_tsquery('english', $1)
	`

	var total int32
	err := r.db.QueryRowContext(ctx, countQuery, query).Scan(&total)
	if err != nil {
		r.log.Error(ctx, "Failed to count search results", map[string]interface{}{"error": err.Error()})
		return nil, 0, fmt.Errorf("failed to count search results: %w", err)
	}

	// Search products
	searchQuery := `
		SELECT id, name, description, price_cents, sku, stock, images, category, created_at, updated_at
		FROM products
		WHERE search_vector @@ websearch_to_tsquery('english', $1)
		ORDER BY ts_rank(search_vector, websearch_to_tsquery('english', $1)) DESC, created_at DESC
		LIMIT $2 OFFSET $3
	`

	rows, err := r.db.QueryContext(ctx, searchQuery, query, pageSize, offset)
	if err != nil {
		r.log.Error(ctx, "Failed to search products", map[string]interface{}{"error": err.Error()})
		return nil, 0, fmt.Errorf("failed to search products: %w", err)
	}
	defer rows.Close()

	products := []*Product{}
	for rows.Next() {
		product := &Product{}
		var images pq.StringArray

		err := rows.Scan(
			&product.ID,
			&product.Name,
			&product.Description,
			&product.PriceCents,
			&product.SKU,
			&product.Stock,
			&images,
			&product.Category,
			&product.CreatedAt,
			&product.UpdatedAt,
		)
		if err != nil {
			r.log.Error(ctx, "Failed to scan search result", map[string]interface{}{"error": err.Error()})
			return nil, 0, fmt.Errorf("failed to scan search result: %w", err)
		}

		product.Images = images
		products = append(products, product)
	}

	if err = rows.Err(); err != nil {
		r.log.Error(ctx, "Error iterating search results", map[string]interface{}{"error": err.Error()})
		return nil, 0, fmt.Errorf("error iterating search results: %w", err)
	}

	r.log.Info(ctx, "Products searched successfully", map[string]interface{}{"query": query, "count": len(products), "total": total})
	return products, total, nil
}

// Close closes the database connection
func (r *postgresRepository) Close() error {
	return r.db.Close()
}

// isInvalidID reports a malformed UUID, which callers should treat the same as a missing product.
func isInvalidID(err error) bool {
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == "22P02"
}

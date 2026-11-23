# Catalog Service - Database Schema

## Overview
The Catalog service uses PostgreSQL to store product information. This document provides a comprehensive reference for the database schema.

## Tables

### products

Stores product information including details, pricing, inventory, and images.

#### Schema Definition

```sql
CREATE TABLE IF NOT EXISTS products (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    price_cents BIGINT NOT NULL CHECK (price_cents >= 0),
    sku VARCHAR(100) UNIQUE NOT NULL,
    stock INTEGER NOT NULL DEFAULT 0 CHECK (stock >= 0),
    images TEXT[],
    category VARCHAR(100),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);
```

#### Columns

| Column | Type | Constraints | Default | Description |
|--------|------|-------------|---------|-------------|
| `id` | VARCHAR(36) | PRIMARY KEY | - | UUID identifier for the product |
| `name` | VARCHAR(255) | NOT NULL | - | Product name |
| `description` | TEXT | - | - | Product description (optional) |
| `price_cents` | BIGINT | NOT NULL, CHECK | - | Product price in cents (integer minor units) |
| `sku` | VARCHAR(100) | UNIQUE NOT NULL | - | Stock Keeping Unit (unique identifier) |
| `stock` | INTEGER | NOT NULL, CHECK | 0 | Available inventory count |
| `images` | TEXT[] | - | - | Array of image URLs |
| `category` | VARCHAR(100) | - | - | Product category (optional) |
| `created_at` | TIMESTAMP WITH TIME ZONE | - | CURRENT_TIMESTAMP | Product creation timestamp |
| `updated_at` | TIMESTAMP WITH TIME ZONE | - | CURRENT_TIMESTAMP | Last update timestamp |

#### Constraints

- **Primary Key**: `id` - Unique identifier
- **Unique**: `sku` - Ensures no duplicate SKUs
- **Check Constraint**: `price_cents >= 0` - Enforces non-negative prices
- **Check Constraint**: `stock >= 0` - Enforces non-negative stock levels
- **Not Null**: `name`, `price_cents`, `sku`, `stock` - Required fields

#### Indexes

```sql
-- SKU index for fast product lookups
CREATE INDEX idx_products_sku ON products(sku);

-- Category index for filtering products by category
CREATE INDEX idx_products_category ON products(category);

-- Name index for product search
CREATE INDEX idx_products_name ON products(name);
```

| Index Name | Column(s) | Purpose |
|------------|-----------|---------|
| `idx_products_sku` | sku | Fast product lookup by SKU |
| `idx_products_category` | category | Efficiently filter products by category |
| `idx_products_name` | name | Support product name search queries |

### Full-text search

`products.search_vector` is a generated `tsvector` column: the name is weighted `A`, the description `B`. A GIN index (`idx_products_search_vector`) serves `SearchProducts`, which matches with `websearch_to_tsquery('english', $1)` and orders by `ts_rank`.

### stock_reservations / stock_reservation_items

One reservation per order, holding the stock and the price each item was sold at.

```sql
CREATE TABLE stock_reservations (
    order_id UUID PRIMARY KEY,
    status VARCHAR(20) NOT NULL,   -- RESERVED, COMMITTED, RELEASED, EXPIRED
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE stock_reservation_items (
    order_id UUID NOT NULL REFERENCES stock_reservations(order_id) ON DELETE CASCADE,
    product_id UUID NOT NULL,
    product_name VARCHAR(255) NOT NULL,
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    unit_price_cents BIGINT NOT NULL CHECK (unit_price_cents >= 0),
    PRIMARY KEY (order_id, product_id)
);
```

| Status | Meaning |
|--------|---------|
| `RESERVED` | Stock taken by `ReserveStock`; the order's `order.created` event has not arrived yet |
| `COMMITTED` | `order.created` arrived, so the order exists |
| `RELEASED` | The order was cancelled and the stock returned |
| `EXPIRED` | No `order.created` within `RESERVATION_TTL` (default 15m), so the order failed to save; the sweeper returned the stock. A late `order.created` re-reserves if stock allows |

Every transition is a guarded `UPDATE ... WHERE status = ...`, so retries and redelivered events are harmless. Rows are locked in product-id order to avoid deadlocks between concurrent orders.

## Migration History

Migrations are embedded in the binary and applied at startup; applied versions are tracked in `catalog_schema_migrations`.

| Migration | File | Description |
|-----------|------|-------------|
| 001 | `001_create_products_table.up.sql` | Initial table creation with all fields and indexes |
| 002 | `002_create_stock_reservations.up.sql` | Per-order stock reservations |
| 003 | `003_add_product_search_vector.up.sql` | Weighted `tsvector` column and GIN index for search |
| 004 | `004_reservation_lifecycle.up.sql` | `COMMITTED`/`EXPIRED` statuses and the sweeper index |
| 005 | `005_prices_in_cents.up.sql` | Prices stored as integer cents |

## Data Types and Formats

### ID Format
- **Type**: UUID v4
- **Format**: `xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx`
- **Example**: `550e8400-e29b-41d4-a716-446655440000`
- **Generation**: `github.com/google/uuid`

### Price Format
- **Type**: BIGINT, integer cents (migration `005_prices_in_cents` converted the old DECIMAL column)
- **Range**: 1 to 1,000,000,000,000 (the service rejects anything else)
- **Example**: `2999` ($29.99), `149900` ($1,499.00)

### SKU Format
- **Type**: VARCHAR(100)
- **Constraints**: Must be unique, non-empty
- **Example**: `LAPTOP-DEL-XPS15`, `PHONE-IPH-14PRO`
- **Recommendation**: Use consistent naming convention for your organization

### Images Array Format
- **Type**: TEXT[] (PostgreSQL array)
- **Storage**: Array of image URLs
- **Example**: `{"https://cdn.example.com/img1.jpg", "https://cdn.example.com/img2.jpg"}`
- **Access**: Using `lib/pq` driver with `pq.Array()` for marshaling/unmarshaling

### Stock Format
- **Type**: INTEGER
- **Range**: 0 to 2,147,483,647
- **Example**: `0` (out of stock), `150` (in stock)

## Query Examples

### Create Product
```sql
INSERT INTO products (id, name, description, price_cents, sku, stock, images, category, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
RETURNING id, name, description, price_cents, sku, stock, images, category, created_at, updated_at;
```

### Get Product by SKU
```sql
SELECT id, name, description, price_cents, sku, stock, images, category, created_at, updated_at
FROM products
WHERE sku = $1;
```

### List Products with Category Filter and Pagination
```sql
SELECT id, name, description, price_cents, sku, stock, images, category, created_at, updated_at
FROM products
WHERE category = $1
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;
```

### Search Products
```sql
SELECT id, name, description, price_cents, sku, stock, images, category, created_at, updated_at
FROM products
WHERE search_vector @@ websearch_to_tsquery('english', $1)
ORDER BY ts_rank(search_vector, websearch_to_tsquery('english', $1)) DESC, created_at DESC
LIMIT $2 OFFSET $3;
```

### Update Product (SKU Immutable)
```sql
UPDATE products
SET name = $2, description = $3, price_cents = $4, stock = $5, images = $6, category = $7, updated_at = $8
WHERE id = $1
RETURNING id, name, description, price_cents, sku, stock, images, category, created_at, updated_at;
```

## Business Rules

1. **Price Validation**: The service requires 0 < price_cents <= 1e12; the column itself only enforces >= 0
2. **Stock Validation**: Stock levels must be non-negative (>= 0)
3. **SKU Uniqueness**: Each product must have a unique SKU
4. **SKU Immutability**: SKU cannot be changed after product creation
5. **Name Requirement**: Product name is required and cannot be empty
6. **Images**: Optional array field, can be empty or contain multiple URLs
7. **Category**: Optional field for product categorization
8. **Timestamps**: Automatically managed by the database

## Performance Considerations

1. **Indexes**: Three indexes (SKU, category, name) to optimize common queries
2. **Category Filter**: Category index enables efficient filtering in list queries
3. **Search**: Name index supports case-insensitive ILIKE searches
4. **Pagination**: LIMIT/OFFSET used for efficient pagination
5. **Array Storage**: TEXT[] for images is efficient for small-to-medium arrays (< 100 items)

## Security Considerations

1. **Input Validation**: All inputs validated before database insertion
2. **SQL Injection**: Using parameterized queries ($1, $2, etc.) prevents SQL injection
3. **Price Constraints**: CHECK constraint prevents negative prices
4. **Stock Constraints**: CHECK constraint prevents negative stock levels

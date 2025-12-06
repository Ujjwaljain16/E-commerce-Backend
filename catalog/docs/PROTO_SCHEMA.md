# Catalog Service - Protocol Buffer Schema

## Overview
The Catalog service uses Protocol Buffers (proto3) for gRPC service definitions. This document provides a comprehensive reference for all messages and RPC methods.

## Proto Package
- **Syntax**: `proto3`
- **Package**: `catalog`
- **Go Package**: `github.com/Ujjwaljain16/E-commerce-Backend/catalog/pb`

## Service Definition

### CatalogService

The main gRPC service providing product catalog management.

```protobuf
service CatalogService {
  rpc CreateProduct(CreateProductRequest) returns (CreateProductResponse);
  rpc GetProduct(GetProductRequest) returns (GetProductResponse);
  rpc ListProducts(ListProductsRequest) returns (ListProductsResponse);
  rpc UpdateProduct(UpdateProductRequest) returns (UpdateProductResponse);
  rpc DeleteProduct(DeleteProductRequest) returns (DeleteProductResponse);
  rpc SearchProducts(SearchProductsRequest) returns (SearchProductsResponse);
}
```

## Message Definitions

### Core Messages

#### Product

Represents a product in the catalog with all details, pricing, and inventory information.

```protobuf
message Product {
  string id = 1;
  string name = 2;
  string description = 3;
  double price = 4;
  string sku = 5;
  int32 stock = 6;
  repeated string images = 7;
  string category = 8;
  google.protobuf.Timestamp created_at = 9;
  google.protobuf.Timestamp updated_at = 10;
}
```

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| `id` | string | 1 | UUID of the product |
| `name` | string | 2 | Product name |
| `description` | string | 3 | Product description (optional) |
| `price` | double | 4 | Product price (must be >= 0) |
| `sku` | string | 5 | Stock Keeping Unit (unique identifier) |
| `stock` | int32 | 6 | Available inventory count (must be >= 0) |
| `images` | repeated string | 7 | Array of image URLs |
| `category` | string | 8 | Product category (optional) |
| `created_at` | Timestamp | 9 | Product creation time (UTC) |
| `updated_at` | Timestamp | 10 | Last modification time (UTC) |

**Notes**:
- `id` is a UUID v4 string
- `sku` is immutable after creation
- `price` stored as DECIMAL(10,2) in database, sent as double
- `images` can be empty array
- Timestamps use `google.protobuf.Timestamp` for interoperability

---

### Product Creation

#### CreateProductRequest

Request to create a new product in the catalog.

```protobuf
message CreateProductRequest {
  string name = 1;
  string description = 2;
  double price = 3;
  string sku = 4;
  int32 stock = 5;
  repeated string images = 6;
  string category = 7;
}
```

| Field | Type | Tag | Required | Validation |
|-------|------|-----|----------|------------|
| `name` | string | 1 | Yes | Non-empty |
| `description` | string | 2 | No | Optional |
| `price` | double | 3 | Yes | Must be > 0 |
| `sku` | string | 4 | Yes | Non-empty, must be unique |
| `stock` | int32 | 5 | Yes | Must be >= 0 |
| `images` | repeated string | 6 | No | Optional array of URLs |
| `category` | string | 7 | No | Optional |

**Error Codes**:
- `InvalidArgument` - Missing required fields, invalid price/stock, or empty name/SKU
- `AlreadyExists` - SKU already exists

#### CreateProductResponse

Response containing the created product.

```protobuf
message CreateProductResponse {
  Product product = 1;
}
```

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| `product` | Product | 1 | Created product object with generated ID and timestamps |

---

### Product Retrieval

#### GetProductRequest

Request to retrieve a specific product by ID.

```protobuf
message GetProductRequest {
  string id = 1;
}
```

| Field | Type | Tag | Required | Description |
|-------|------|-----|----------|-------------|
| `id` | string | 1 | Yes | Product UUID |

**Error Codes**:
- `InvalidArgument` - Missing or empty ID
- `NotFound` - Product not found

#### GetProductResponse

Response containing the requested product.

```protobuf
message GetProductResponse {
  Product product = 1;
}
```

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| `product` | Product | 1 | Retrieved product object |

---

### Product Listing

#### ListProductsRequest

Request to list products with optional filtering and pagination.

```protobuf
message ListProductsRequest {
  int32 page = 1;
  int32 page_size = 2;
  string category = 3;
}
```

| Field | Type | Tag | Required | Description |
|-------|------|-----|----------|-------------|
| `page` | int32 | 1 | No | Page number (default: 1) |
| `page_size` | int32 | 2 | No | Items per page (default: 10) |
| `category` | string | 3 | No | Filter by category (empty = all) |

**Notes**:
- Pagination: OFFSET = (page - 1) * page_size
- Default page_size: 10
- Results ordered by created_at DESC

#### ListProductsResponse

Response containing paginated product list.

```protobuf
message ListProductsResponse {
  repeated Product products = 1;
  int32 total = 2;
}
```

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| `products` | repeated Product | 1 | Array of products for current page |
| `total` | int32 | 2 | Total count of products matching filter |

---

### Product Update

#### UpdateProductRequest

Request to update an existing product.

```protobuf
message UpdateProductRequest {
  string id = 1;
  string name = 2;
  string description = 3;
  double price = 4;
  int32 stock = 5;
  repeated string images = 6;
  string category = 7;
}
```

| Field | Type | Tag | Required | Validation |
|-------|------|-----|----------|------------|
| `id` | string | 1 | Yes | Product UUID |
| `name` | string | 2 | Yes | Non-empty |
| `description` | string | 3 | No | Optional |
| `price` | double | 4 | Yes | Must be > 0 |
| `stock` | int32 | 5 | Yes | Must be >= 0 |
| `images` | repeated string | 6 | No | Optional array of URLs |
| `category` | string | 7 | No | Optional |

**Notes**:
- `sku` is NOT included (immutable)
- All fields except `id` are updated
- `updated_at` is automatically set to current time

**Error Codes**:
- `InvalidArgument` - Missing ID, invalid price/stock, or empty name
- `NotFound` - Product not found

#### UpdateProductResponse

Response containing the updated product.

```protobuf
message UpdateProductResponse {
  Product product = 1;
}
```

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| `product` | Product | 1 | Updated product object with new updated_at timestamp |

---

### Product Deletion

#### DeleteProductRequest

Request to delete a product by ID.

```protobuf
message DeleteProductRequest {
  string id = 1;
}
```

| Field | Type | Tag | Required | Description |
|-------|------|-----|----------|-------------|
| `id` | string | 1 | Yes | Product UUID |

**Error Codes**:
- `InvalidArgument` - Missing or empty ID
- `NotFound` - Product not found

#### DeleteProductResponse

Empty response confirming deletion.

```protobuf
message DeleteProductResponse {}
```

**Notes**:
- Hard delete (permanent removal from database)
- No soft delete implemented
- Returns empty response on success

---

### Product Search

#### SearchProductsRequest

Request to search products by name with pagination.

```protobuf
message SearchProductsRequest {
  string query = 1;
  int32 page = 2;
  int32 page_size = 3;
}
```

| Field | Type | Tag | Required | Description |
|-------|------|-----|----------|-------------|
| `query` | string | 1 | Yes | Search term for product name (case-insensitive) |
| `page` | int32 | 2 | No | Page number (default: 1) |
| `page_size` | int32 | 3 | No | Items per page (default: 10) |

**Notes**:
- Search uses ILIKE for case-insensitive partial matching
- Query wrapped with `%` for substring search
- Results ordered by name ASC

**Error Codes**:
- `InvalidArgument` - Empty query string

#### SearchProductsResponse

Response containing search results with pagination.

```protobuf
message SearchProductsResponse {
  repeated Product products = 1;
  int32 total = 2;
}
```

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| `products` | repeated Product | 1 | Array of products matching search query |
| `total` | int32 | 2 | Total count of products matching search |

---

## RPC Method Summary

| Method | Request | Response | Description |
|--------|---------|----------|-------------|
| `CreateProduct` | CreateProductRequest | CreateProductResponse | Create new product |
| `GetProduct` | GetProductRequest | GetProductResponse | Get product by ID |
| `ListProducts` | ListProductsRequest | ListProductsResponse | List products with filtering/pagination |
| `UpdateProduct` | UpdateProductRequest | UpdateProductResponse | Update existing product |
| `DeleteProduct` | DeleteProductRequest | DeleteProductResponse | Delete product by ID |
| `SearchProducts` | SearchProductsRequest | SearchProductsResponse | Search products by name |

## Error Handling

### gRPC Status Codes

| Code | Usage | Example |
|------|-------|---------|
| `OK` | Success | All successful operations |
| `InvalidArgument` | Bad input | Missing name, negative price, empty SKU |
| `NotFound` | Resource missing | Product ID not found |
| `AlreadyExists` | Duplicate resource | SKU already exists |
| `Internal` | Server error | Database connection failure |

### Validation Rules

#### Name
- **Required**: Yes
- **Constraints**: Non-empty string
- **Max Length**: 255 characters

#### Price
- **Required**: Yes
- **Constraints**: Must be > 0
- **Type**: double (2 decimal places in DB)

#### SKU
- **Required**: Yes (on create)
- **Constraints**: Non-empty, unique
- **Immutable**: Cannot be changed after creation
- **Max Length**: 100 characters

#### Stock
- **Required**: Yes
- **Constraints**: Must be >= 0
- **Type**: int32

#### Images
- **Required**: No
- **Type**: Array of strings (URLs)
- **Constraints**: Each URL should be valid

#### Category
- **Required**: No
- **Max Length**: 100 characters

## Usage Examples

### Create Product
```go
req := &pb.CreateProductRequest{
    Name:        "Laptop",
    Description: "High-performance laptop",
    Price:       1299.99,
    Sku:         "LAPTOP-001",
    Stock:       50,
    Images:      []string{"https://example.com/img1.jpg"},
    Category:    "Electronics",
}
resp, err := client.CreateProduct(ctx, req)
```

### List Products with Category Filter
```go
req := &pb.ListProductsRequest{
    Page:     1,
    PageSize: 20,
    Category: "Electronics",
}
resp, err := client.ListProducts(ctx, req)
```

### Search Products
```go
req := &pb.SearchProductsRequest{
    Query:    "laptop",
    Page:     1,
    PageSize: 10,
}
resp, err := client.SearchProducts(ctx, req)
```

### Update Product
```go
req := &pb.UpdateProductRequest{
    Id:          "550e8400-e29b-41d4-a716-446655440000",
    Name:        "Updated Laptop",
    Price:       1199.99,
    Stock:       45,
    Images:      []string{"https://example.com/new.jpg"},
    Category:    "Electronics",
}
resp, err := client.UpdateProduct(ctx, req)
```

## Best Practices

1. **SKU Management**: Use consistent SKU naming conventions across your organization
2. **Image URLs**: Store absolute URLs pointing to CDN or image service
3. **Price Handling**: Use `math.Round(price * 100) / 100` for 2 decimal precision
4. **Pagination**: Use reasonable page_size (10-100) to balance performance and UX
5. **Search**: Sanitize search queries to prevent injection attacks
6. **Error Handling**: Always check gRPC status codes in client applications
7. **Timestamps**: Use UTC for all timestamps to avoid timezone issues

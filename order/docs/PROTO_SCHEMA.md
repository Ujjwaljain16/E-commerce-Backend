# Order Service - Protocol Buffer Schema

## Overview
The Order service uses Protocol Buffers (proto3) for gRPC service definitions. This document provides a comprehensive reference for all messages and RPC methods.

## Proto Package
- **Syntax**: `proto3`
- **Package**: `order`
- **Go Package**: `github.com/Ujjwaljain16/E-commerce-Backend/order/pb`

## Service Definition

### OrderService

The main gRPC service providing order management.

```protobuf
service OrderService {
    rpc CreateOrder(CreateOrderRequest) returns (CreateOrderResponse);
    rpc GetOrder(GetOrderRequest) returns (GetOrderResponse);
    rpc ListOrders(ListOrdersRequest) returns (ListOrdersResponse);
    rpc UpdateOrderStatus(UpdateOrderStatusRequest) returns (UpdateOrderStatusResponse);
    rpc CancelOrder(CancelOrderRequest) returns (CancelOrderResponse);
    rpc GetOrdersByUser(GetOrdersByUserRequest) returns (GetOrdersByUserResponse);
}
```

## Message Definitions

### Core Messages

#### Order

Represents a customer order.

```protobuf
message Order {
    string id = 1;
    string user_id = 2;
    OrderStatus status = 3;
    repeated OrderItem items = 4;
    double total_amount = 5;
    string shipping_address = 6;
    string billing_address = 7;
    string payment_method = 8;
    google.protobuf.Timestamp created_at = 9;
    google.protobuf.Timestamp updated_at = 10;
}
```

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| `id` | string | 1 | UUID of the order |
| `user_id` | string | 2 | UUID of the user who placed the order |
| `status` | OrderStatus | 3 | Current status of the order |
| `items` | repeated OrderItem | 4 | List of items in the order |
| `total_amount` | double | 5 | Total cost of the order |
| `shipping_address` | string | 6 | Shipping address |
| `billing_address` | string | 7 | Billing address |
| `payment_method` | string | 8 | Payment method (e.g., "credit_card") |
| `created_at` | Timestamp | 9 | Order creation time (UTC) |
| `updated_at` | Timestamp | 10 | Last modification time (UTC) |

#### OrderItem

Represents a single item in an order.

```protobuf
message OrderItem {
    string id = 1;
    string order_id = 2;
    string product_id = 3;
    string product_name = 4;
    int32 quantity = 5;
    double price = 6;
    double subtotal = 7;
}
```

| Field | Type | Tag | Description |
|-------|------|-----|-------------|
| `id` | string | 1 | UUID of the order item |
| `order_id` | string | 2 | UUID of the parent order |
| `product_id` | string | 3 | UUID of the product |
| `product_name` | string | 4 | Name of the product at time of purchase |
| `quantity` | int32 | 5 | Quantity purchased |
| `price` | double | 6 | Price per unit at time of purchase |
| `subtotal` | double | 7 | Total price for this line item |

#### OrderStatus

Enum representing the state of an order.

```protobuf
enum OrderStatus {
    ORDER_STATUS_UNSPECIFIED = 0;
    PENDING = 1;
    CONFIRMED = 2;
    PROCESSING = 3;
    SHIPPED = 4;
    DELIVERED = 5;
    CANCELLED = 6;
    REFUNDED = 7;
}
```

### Order Creation

#### CreateOrderRequest

```protobuf
message CreateOrderRequest {
    string user_id = 1;
    repeated OrderItemInput items = 2;
    string shipping_address = 3;
    string billing_address = 4;
    string payment_method = 5;
}
```

#### OrderItemInput

```protobuf
message OrderItemInput {
    string product_id = 1;
    string product_name = 2;
    int32 quantity = 3;
    double price = 4;
}
```

#### CreateOrderResponse

```protobuf
message CreateOrderResponse {
    Order order = 1;
}
```

### Order Retrieval

#### GetOrderRequest

```protobuf
message GetOrderRequest {
    string id = 1;
}
```

#### GetOrderResponse

```protobuf
message GetOrderResponse {
    Order order = 1;
}
```

### Order Listing

#### ListOrdersRequest

```protobuf
message ListOrdersRequest {
    int32 page = 1;
    int32 page_size = 2;
    string user_id = 3;
    OrderStatus status = 4;
}
```

#### ListOrdersResponse

```protobuf
message ListOrdersResponse {
    repeated Order orders = 1;
    int32 total = 2;
    int32 page = 3;
    int32 page_size = 4;
}
```

### Order Status Update

#### UpdateOrderStatusRequest

```protobuf
message UpdateOrderStatusRequest {
    string id = 1;
    OrderStatus status = 2;
}
```

#### UpdateOrderStatusResponse

```protobuf
message UpdateOrderStatusResponse {
    Order order = 1;
}
```

### Order Cancellation

#### CancelOrderRequest

```protobuf
message CancelOrderRequest {
    string id = 1;
    string reason = 2;
}
```

#### CancelOrderResponse

```protobuf
message CancelOrderResponse {
    bool success = 1;
    string message = 2;
}
```

### User Orders

#### GetOrdersByUserRequest

```protobuf
message GetOrdersByUserRequest {
    string user_id = 1;
    int32 page = 2;
    int32 page_size = 3;
}
```

#### GetOrdersByUserResponse

```protobuf
message GetOrdersByUserResponse {
    repeated Order orders = 1;
    int32 total = 2;
    int32 page = 3;
    int32 page_size = 4;
}
```

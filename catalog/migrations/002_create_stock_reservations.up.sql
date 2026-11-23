-- One row per order; makes ReserveStock/ReleaseStock idempotent.
CREATE TABLE IF NOT EXISTS stock_reservations (
    order_id UUID PRIMARY KEY,
    status VARCHAR(20) NOT NULL CHECK (status IN ('RESERVED', 'RELEASED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS stock_reservation_items (
    order_id UUID NOT NULL REFERENCES stock_reservations(order_id) ON DELETE CASCADE,
    product_id UUID NOT NULL,
    product_name VARCHAR(255) NOT NULL,
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    unit_price DECIMAL(10, 2) NOT NULL CHECK (unit_price >= 0),
    PRIMARY KEY (order_id, product_id)
);

-- Orders remember the currency they were priced in, so later events (order.confirmed) can carry it.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS currency VARCHAR(3) NOT NULL DEFAULT 'USD';

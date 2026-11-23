ALTER TABLE order_items DROP CONSTRAINT IF EXISTS order_items_price_cents_check;
ALTER TABLE order_items DROP CONSTRAINT IF EXISTS order_items_subtotal_cents_check;
ALTER TABLE order_items RENAME COLUMN price_cents TO price;
ALTER TABLE order_items RENAME COLUMN subtotal_cents TO subtotal;
ALTER TABLE order_items ALTER COLUMN price TYPE DECIMAL(10,2) USING price / 100.0;
ALTER TABLE order_items ALTER COLUMN subtotal TYPE DECIMAL(10,2) USING subtotal / 100.0;
ALTER TABLE order_items ADD CONSTRAINT order_items_price_check CHECK (price >= 0);
ALTER TABLE order_items ADD CONSTRAINT order_items_subtotal_check CHECK (subtotal >= 0);

ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_total_amount_cents_check;
ALTER TABLE orders RENAME COLUMN total_amount_cents TO total_amount;
ALTER TABLE orders ALTER COLUMN total_amount TYPE DECIMAL(10,2) USING total_amount / 100.0;
ALTER TABLE orders ADD CONSTRAINT orders_total_amount_check CHECK (total_amount >= 0);

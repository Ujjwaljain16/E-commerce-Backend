-- Store money as integer cents so totals never pass through floating point.
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_total_amount_check;
ALTER TABLE orders ALTER COLUMN total_amount TYPE BIGINT USING round(total_amount * 100)::bigint;
ALTER TABLE orders RENAME COLUMN total_amount TO total_amount_cents;
ALTER TABLE orders ADD CONSTRAINT orders_total_amount_cents_check CHECK (total_amount_cents >= 0);

ALTER TABLE order_items DROP CONSTRAINT IF EXISTS order_items_price_check;
ALTER TABLE order_items DROP CONSTRAINT IF EXISTS order_items_subtotal_check;
ALTER TABLE order_items ALTER COLUMN price TYPE BIGINT USING round(price * 100)::bigint;
ALTER TABLE order_items ALTER COLUMN subtotal TYPE BIGINT USING round(subtotal * 100)::bigint;
ALTER TABLE order_items RENAME COLUMN price TO price_cents;
ALTER TABLE order_items RENAME COLUMN subtotal TO subtotal_cents;
ALTER TABLE order_items ADD CONSTRAINT order_items_price_cents_check CHECK (price_cents >= 0);
ALTER TABLE order_items ADD CONSTRAINT order_items_subtotal_cents_check CHECK (subtotal_cents >= 0);

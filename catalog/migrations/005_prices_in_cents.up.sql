-- Store money as integer cents so prices never pass through floating point.
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_price_check;
ALTER TABLE products ALTER COLUMN price TYPE BIGINT USING round(price * 100)::bigint;
ALTER TABLE products RENAME COLUMN price TO price_cents;
ALTER TABLE products ADD CONSTRAINT products_price_cents_check CHECK (price_cents >= 0);

ALTER TABLE stock_reservation_items DROP CONSTRAINT IF EXISTS stock_reservation_items_unit_price_check;
ALTER TABLE stock_reservation_items ALTER COLUMN unit_price TYPE BIGINT USING round(unit_price * 100)::bigint;
ALTER TABLE stock_reservation_items RENAME COLUMN unit_price TO unit_price_cents;
ALTER TABLE stock_reservation_items ADD CONSTRAINT stock_reservation_items_unit_price_cents_check
    CHECK (unit_price_cents >= 0);

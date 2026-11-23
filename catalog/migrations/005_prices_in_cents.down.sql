ALTER TABLE stock_reservation_items DROP CONSTRAINT IF EXISTS stock_reservation_items_unit_price_cents_check;
ALTER TABLE stock_reservation_items RENAME COLUMN unit_price_cents TO unit_price;
ALTER TABLE stock_reservation_items ALTER COLUMN unit_price TYPE DECIMAL(10, 2) USING unit_price / 100.0;
ALTER TABLE stock_reservation_items ADD CONSTRAINT stock_reservation_items_unit_price_check CHECK (unit_price >= 0);

ALTER TABLE products DROP CONSTRAINT IF EXISTS products_price_cents_check;
ALTER TABLE products RENAME COLUMN price_cents TO price;
ALTER TABLE products ALTER COLUMN price TYPE DECIMAL(10, 2) USING price / 100.0;
ALTER TABLE products ADD CONSTRAINT products_price_check CHECK (price >= 0);

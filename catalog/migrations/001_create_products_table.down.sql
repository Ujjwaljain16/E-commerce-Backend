DROP TRIGGER IF EXISTS trigger_update_products_updated_at ON products;
DROP FUNCTION IF EXISTS update_products_updated_at();
DROP INDEX IF EXISTS idx_products_name;
DROP INDEX IF EXISTS idx_products_category;
DROP INDEX IF EXISTS idx_products_sku;
DROP TABLE IF EXISTS products;

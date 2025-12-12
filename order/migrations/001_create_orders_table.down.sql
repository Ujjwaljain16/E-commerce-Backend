-- Drop triggers first
DROP TRIGGER IF EXISTS trigger_update_orders_updated_at ON orders;

-- Drop function
DROP FUNCTION IF EXISTS update_orders_updated_at();

-- Drop indexes
DROP INDEX IF EXISTS idx_order_items_product_id;
DROP INDEX IF EXISTS idx_order_items_order_id;
DROP INDEX IF EXISTS idx_orders_created_at;
DROP INDEX IF EXISTS idx_orders_status;
DROP INDEX IF EXISTS idx_orders_user_id;

-- Drop tables (order_items first due to foreign key)
DROP TABLE IF EXISTS order_items;
DROP TABLE IF EXISTS orders;

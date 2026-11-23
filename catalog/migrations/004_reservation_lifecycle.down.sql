DROP INDEX IF EXISTS idx_stock_reservations_pending;
UPDATE stock_reservations SET status = 'RELEASED' WHERE status = 'EXPIRED';
UPDATE stock_reservations SET status = 'RESERVED' WHERE status = 'COMMITTED';
ALTER TABLE stock_reservations DROP CONSTRAINT IF EXISTS stock_reservations_status_check;
ALTER TABLE stock_reservations ADD CONSTRAINT stock_reservations_status_check
    CHECK (status IN ('RESERVED', 'RELEASED'));

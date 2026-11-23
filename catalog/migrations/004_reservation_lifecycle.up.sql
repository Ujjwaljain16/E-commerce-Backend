-- COMMITTED: the order exists. EXPIRED: no order appeared within the TTL, stock was returned.
ALTER TABLE stock_reservations DROP CONSTRAINT IF EXISTS stock_reservations_status_check;
ALTER TABLE stock_reservations ADD CONSTRAINT stock_reservations_status_check
    CHECK (status IN ('RESERVED', 'COMMITTED', 'RELEASED', 'EXPIRED'));

-- The sweeper only ever looks for old RESERVED rows.
CREATE INDEX IF NOT EXISTS idx_stock_reservations_pending
    ON stock_reservations (created_at) WHERE status = 'RESERVED';

-- COMMITTED: the order exists. EXPIRED: no order appeared within the TTL, stock was returned.
ALTER TABLE stock_reservations DROP CONSTRAINT IF EXISTS stock_reservations_status_check;
ALTER TABLE stock_reservations ADD CONSTRAINT stock_reservations_status_check
    CHECK (status IN ('RESERVED', 'COMMITTED', 'RELEASED', 'EXPIRED'));

-- Reservations made before this migration never get a commit (their order.created events were
-- already consumed), so without this the sweeper would hand their stock back and oversell.
UPDATE stock_reservations SET status = 'COMMITTED', updated_at = NOW() WHERE status = 'RESERVED';

-- The sweeper only ever looks for old RESERVED rows.
CREATE INDEX IF NOT EXISTS idx_stock_reservations_pending
    ON stock_reservations (created_at) WHERE status = 'RESERVED';

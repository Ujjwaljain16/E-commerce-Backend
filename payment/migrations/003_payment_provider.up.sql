-- Which provider handles a payment, and for Razorpay the order ID that Checkout is opened with.
ALTER TABLE payments ADD COLUMN IF NOT EXISTS provider VARCHAR(20) NOT NULL DEFAULT 'mock';
ALTER TABLE payments ADD COLUMN IF NOT EXISTS provider_order_id VARCHAR(64);

-- Webhooks look payments up by the provider's order ID.
CREATE UNIQUE INDEX IF NOT EXISTS idx_payments_provider_order_id ON payments (provider_order_id)
    WHERE provider_order_id IS NOT NULL;

-- The expiry sweeper only scans payments still waiting for the customer.
CREATE INDEX IF NOT EXISTS idx_payments_pending_created_at ON payments (created_at)
    WHERE status = 1;

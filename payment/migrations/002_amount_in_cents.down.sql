ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_amount_cents_check;
ALTER TABLE payments RENAME COLUMN amount_cents TO amount;
ALTER TABLE payments ALTER COLUMN amount TYPE NUMERIC(12,2) USING amount / 100.0;
ALTER TABLE payments ADD CONSTRAINT payments_amount_check CHECK (amount >= 0);

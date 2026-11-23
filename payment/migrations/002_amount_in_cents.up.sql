-- Store money as integer cents so charged amounts never pass through floating point.
ALTER TABLE payments DROP CONSTRAINT IF EXISTS payments_amount_check;
ALTER TABLE payments ALTER COLUMN amount TYPE BIGINT USING round(amount * 100)::bigint;
ALTER TABLE payments RENAME COLUMN amount TO amount_cents;
ALTER TABLE payments ADD CONSTRAINT payments_amount_cents_check CHECK (amount_cents >= 0);

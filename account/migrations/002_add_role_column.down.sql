-- Remove role-related constraints and column
DROP INDEX IF EXISTS idx_accounts_role;
ALTER TABLE accounts DROP CONSTRAINT IF EXISTS accounts_role_check;
ALTER TABLE accounts DROP COLUMN IF EXISTS role;

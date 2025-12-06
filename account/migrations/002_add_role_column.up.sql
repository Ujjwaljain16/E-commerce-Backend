-- Add role column to accounts table
ALTER TABLE accounts ADD COLUMN role VARCHAR(20) NOT NULL DEFAULT 'USER';

-- Add check constraint to ensure valid roles
ALTER TABLE accounts ADD CONSTRAINT accounts_role_check 
    CHECK (role IN ('USER', 'ADMIN'));

-- Add index on role for efficient queries
CREATE INDEX idx_accounts_role ON accounts(role);

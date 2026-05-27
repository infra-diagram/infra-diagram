-- AWS accounts registered in the system.
-- Each row maps a UUID (used in API) to an AWS account ID (used in Neo4j).
CREATE TABLE IF NOT EXISTS aws_accounts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    aws_account_id  VARCHAR(12) NOT NULL UNIQUE,
    alias           VARCHAR(255) NOT NULL,
    status          VARCHAR(50) NOT NULL DEFAULT 'active',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_aws_accounts_aws_account_id ON aws_accounts(aws_account_id);
CREATE INDEX IF NOT EXISTS idx_aws_accounts_status ON aws_accounts(status);
-- Dashboard and health analytics filter usage_records by tenant/time and then
-- group by provider/model/account/client. These composite indexes keep common
-- production dashboard loads off full table scans as SQLite files grow.
CREATE INDEX IF NOT EXISTS idx_usage_tenant_time_desc
    ON usage_records(tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_usage_tenant_time_provider
    ON usage_records(tenant_id, created_at, provider);

CREATE INDEX IF NOT EXISTS idx_usage_tenant_time_provider_model
    ON usage_records(tenant_id, created_at, provider, model);

CREATE INDEX IF NOT EXISTS idx_usage_tenant_time_account
    ON usage_records(tenant_id, created_at, account_id);

CREATE INDEX IF NOT EXISTS idx_usage_tenant_time_client
    ON usage_records(tenant_id, created_at, client);

CREATE INDEX IF NOT EXISTS idx_usage_tenant_time_account_provider_model
    ON usage_records(tenant_id, created_at, account_id, provider, model);

CREATE INDEX IF NOT EXISTS idx_usage_tenant_time_slim_rules
    ON usage_records(tenant_id, created_at, slim_rules);
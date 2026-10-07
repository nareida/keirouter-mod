-- Actionable provider health dashboard: current state, historical snapshots,
-- and synthetic probe results. See docs/spec provider-health-dashboard.
--
-- Portable across SQLite and PostgreSQL: TEXT ids/timestamps (RFC3339),
-- INTEGER counters, REAL rates. Nullable dimensions use '' (empty string)
-- rather than NULL so UNIQUE constraints behave identically across engines.

-- Current rolled-up health per provider/account/model/capability key.
-- Updated by both the telemetry aggregator (real traffic) and the probe
-- worker (synthetic probes). Fast lookup for the dashboard overview.
CREATE TABLE IF NOT EXISTS provider_health_current (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    provider_account_id TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    capability TEXT NOT NULL DEFAULT '',
    health_status TEXT NOT NULL DEFAULT 'unknown',
    health_score INTEGER NOT NULL DEFAULT 100,
    success_rate REAL NOT NULL DEFAULT 0,
    error_rate REAL NOT NULL DEFAULT 0,
    request_count INTEGER NOT NULL DEFAULT 0,
    fallback_count INTEGER NOT NULL DEFAULT 0,
    latency_p95_ms INTEGER,
    ttft_p95_ms INTEGER,
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    main_issue TEXT,
    recommendation TEXT,
    last_success_at TEXT,
    last_failure_at TEXT,
    last_probe_at TEXT,
    last_updated_at TEXT NOT NULL,
    health_key TEXT NOT NULL UNIQUE
);

CREATE INDEX IF NOT EXISTS idx_provider_health_current_status ON provider_health_current(health_status);
CREATE INDEX IF NOT EXISTS idx_provider_health_current_provider ON provider_health_current(provider);

-- Aggregated health buckets for historical charts and trend analysis.
-- One row per (bucket_start, provider, account, model, capability).
CREATE TABLE IF NOT EXISTS provider_health_snapshots (
    id TEXT PRIMARY KEY,
    bucket_start TEXT NOT NULL,
    bucket_size_seconds INTEGER NOT NULL,
    provider TEXT NOT NULL,
    provider_account_id TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL DEFAULT '',
    capability TEXT NOT NULL DEFAULT '',
    request_count INTEGER NOT NULL DEFAULT 0,
    success_count INTEGER NOT NULL DEFAULT 0,
    failure_count INTEGER NOT NULL DEFAULT 0,
    fallback_count INTEGER NOT NULL DEFAULT 0,
    final_failure_count INTEGER NOT NULL DEFAULT 0,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    estimated_cost_microusd INTEGER NOT NULL DEFAULT 0,
    latency_p50_ms INTEGER,
    latency_p95_ms INTEGER,
    latency_p99_ms INTEGER,
    ttft_p50_ms INTEGER,
    ttft_p95_ms INTEGER,
    ttft_p99_ms INTEGER,
    rate_limited_count INTEGER NOT NULL DEFAULT 0,
    auth_error_count INTEGER NOT NULL DEFAULT 0,
    quota_exceeded_count INTEGER NOT NULL DEFAULT 0,
    timeout_count INTEGER NOT NULL DEFAULT 0,
    provider_5xx_count INTEGER NOT NULL DEFAULT 0,
    bad_request_count INTEGER NOT NULL DEFAULT 0,
    network_error_count INTEGER NOT NULL DEFAULT 0,
    unsupported_count INTEGER NOT NULL DEFAULT 0,
    unknown_error_count INTEGER NOT NULL DEFAULT 0,
    health_score INTEGER NOT NULL DEFAULT 100,
    health_status TEXT NOT NULL DEFAULT 'unknown',
    main_issue TEXT,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_provider_health_snapshots_lookup
    ON provider_health_snapshots(bucket_start, provider, provider_account_id, model, capability);
CREATE INDEX IF NOT EXISTS idx_provider_health_snapshots_status
    ON provider_health_snapshots(health_status, bucket_start);

-- Synthetic probe results (scheduled + manual). Drives the probe history page
-- and feeds provider_health_current when no real traffic exists.
CREATE TABLE IF NOT EXISTS provider_probe_results (
    id TEXT PRIMARY KEY,
    provider TEXT NOT NULL,
    provider_account_id TEXT NOT NULL,
    model TEXT NOT NULL,
    capability TEXT NOT NULL,
    status TEXT NOT NULL,
    http_status INTEGER,
    latency_ms INTEGER,
    ttft_ms INTEGER,
    error_type TEXT,
    error_message TEXT,
    prompt_tokens INTEGER,
    completion_tokens INTEGER,
    estimated_cost_microusd INTEGER,
    triggered_by TEXT NOT NULL DEFAULT 'scheduled',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_provider_probe_results_lookup
    ON provider_probe_results(created_at, provider, provider_account_id, model, capability);

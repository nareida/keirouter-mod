-- Add slim_active flag to usage_records so the RTK badge reflects whether the
-- slimmer was enabled for the request, independent of bytes saved (matches the
-- caveman_active / terse_active semantics). DEFAULT0 keeps old records backward
-- compatible (they read as inactive).
ALTER TABLE usage_records ADD COLUMN slim_active INTEGER NOT NULL DEFAULT 0;

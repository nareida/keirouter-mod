-- Add Headroom (input-side proxy compression) and Ponytail (output-side
-- system-prompt injection) savings columns to usage_records.
-- Tracks Headroom non-phantom token/byte savings plus Headroom/Ponytail
-- activation flags for detailed savings reporting. DEFAULT 0 keeps old records
-- backward compatible (they read as zero/false).
ALTER TABLE usage_records ADD COLUMN headroom_tokens_saved INTEGER NOT NULL DEFAULT 0;
ALTER TABLE usage_records ADD COLUMN headroom_bytes_saved INTEGER NOT NULL DEFAULT 0;
ALTER TABLE usage_records ADD COLUMN headroom_active INTEGER NOT NULL DEFAULT 0;
ALTER TABLE usage_records ADD COLUMN ponytail_active INTEGER NOT NULL DEFAULT 0;

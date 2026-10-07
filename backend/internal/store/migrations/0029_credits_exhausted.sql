-- Add credits_exhausted flag so accounts whose paid balance is depleted are
-- parked (long cooldown) and surfaced in the dashboard instead of thrashing
-- through short quota cooldowns. Cleared on success, provider reconnect, or
-- expiry of the parking cooldown.
ALTER TABLE accounts ADD COLUMN credits_exhausted INTEGER NOT NULL DEFAULT 0;

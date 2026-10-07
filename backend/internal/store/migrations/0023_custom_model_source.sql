-- Distinguish user-entered custom models from models imported via the
-- /models discovery endpoint. Imported models populate the "Available Models"
-- catalog only; the "Custom Models" section shows manual entries exclusively.
ALTER TABLE custom_models ADD COLUMN source TEXT NOT NULL DEFAULT 'manual';

ALTER TABLE sensors
    ADD COLUMN IF NOT EXISTS actual_config_unapplied_fields JSONB NOT NULL DEFAULT '[]'::jsonb;

ALTER TABLE sensors
    ADD COLUMN actual_config_version BIGINT NOT NULL DEFAULT 0;

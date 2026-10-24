CREATE TABLE IF NOT EXISTS sensor_certificates (
    id           BIGSERIAL PRIMARY KEY,
    host_id      TEXT NOT NULL,
    serial       TEXT NOT NULL UNIQUE,
    issued_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL,
    revoked_at   TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS sensor_certificates_host_id ON sensor_certificates (host_id);

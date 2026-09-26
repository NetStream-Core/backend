CREATE TABLE IF NOT EXISTS sensors (
    id             BIGSERIAL PRIMARY KEY,
    host_id        TEXT NOT NULL UNIQUE,
    hostname       TEXT NOT NULL DEFAULT '',
    agent_version  TEXT NOT NULL DEFAULT '',
    config_version BIGINT NOT NULL DEFAULT 0,
    last_seen      TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS agent_configs (
    id         BIGSERIAL PRIMARY KEY,
    sensor_id  BIGINT NOT NULL REFERENCES sensors (id) ON DELETE CASCADE,
    version    BIGINT NOT NULL,
    payload    JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (sensor_id, version)
);

CREATE TABLE IF NOT EXISTS models (
    id               BIGSERIAL PRIMARY KEY,
    name             TEXT NOT NULL,
    version          TEXT NOT NULL,
    algorithm        TEXT NOT NULL DEFAULT '',
    features_version TEXT NOT NULL DEFAULT '',
    dataset_ref      TEXT NOT NULL DEFAULT '',
    git_sha          TEXT NOT NULL DEFAULT '',
    metrics          JSONB NOT NULL DEFAULT '{}'::jsonb,
    artifact_uri     TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'registered',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (name, version)
);

CREATE TABLE IF NOT EXISTS incidents (
    id         BIGSERIAL PRIMARY KEY,
    host_id    TEXT NOT NULL,
    first_seen TIMESTAMPTZ NOT NULL,
    last_seen  TIMESTAMPTZ NOT NULL,
    status     TEXT NOT NULL DEFAULT 'open',
    severity   TEXT NOT NULL DEFAULT 'low',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

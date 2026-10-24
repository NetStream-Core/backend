ALTER TABLE incidents
    ADD COLUMN IF NOT EXISTS src_ip TEXT NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS detection_count BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS max_score DOUBLE PRECISION NOT NULL DEFAULT 0;

-- At most one open incident per (host_id, src_ip): a detection that
-- matches one already-open incident updates it instead of opening a
-- duplicate. A closed incident for the same pair doesn't block a new one
-- — a resolved attack followed by a new one later is a new incident.
CREATE UNIQUE INDEX IF NOT EXISTS incidents_open_host_src_ip
    ON incidents (host_id, src_ip)
    WHERE status = 'open';

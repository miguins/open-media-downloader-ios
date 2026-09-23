CREATE TABLE api_keys (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    secret_hash BLOB NOT NULL,
    created_at INTEGER NOT NULL,
    last_used_at INTEGER,
    revoked_at INTEGER
) STRICT;

CREATE TABLE jobs (
    id TEXT PRIMARY KEY,
    owner_id TEXT NOT NULL REFERENCES api_keys(id),
    source_url TEXT NOT NULL,
    platform TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('queued', 'running', 'succeeded', 'failed', 'canceled')),
    error_code TEXT,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    started_at INTEGER,
    finished_at INTEGER,
    expires_at INTEGER NOT NULL
) STRICT;

CREATE INDEX jobs_owner_id ON jobs (owner_id);
CREATE INDEX jobs_status_created_at ON jobs (status, created_at);
CREATE INDEX jobs_expires_at ON jobs (expires_at);

CREATE TABLE items (
    id TEXT PRIMARY KEY,
    job_id TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    position INTEGER NOT NULL CHECK (position >= 0),
    file_name TEXT NOT NULL,
    media_type TEXT NOT NULL,
    size_bytes INTEGER NOT NULL CHECK (size_bytes >= 0),
    created_at INTEGER NOT NULL,
    UNIQUE (job_id, position)
) STRICT;

CREATE TABLE download_tokens (
    token_hash BLOB PRIMARY KEY,
    item_id TEXT NOT NULL REFERENCES items(id) ON DELETE CASCADE,
    owner_id TEXT NOT NULL REFERENCES api_keys(id),
    created_at INTEGER NOT NULL,
    expires_at INTEGER NOT NULL
) STRICT;

CREATE INDEX download_tokens_item_id ON download_tokens (item_id);
CREATE INDEX download_tokens_expires_at ON download_tokens (expires_at);

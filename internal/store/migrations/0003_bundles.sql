CREATE TABLE bundles (
 job_id TEXT PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,
 file_name TEXT NOT NULL,
 size_bytes INTEGER NOT NULL CHECK (size_bytes > 0),
 created_at INTEGER NOT NULL
) STRICT;

CREATE TABLE bundle_tokens (
 token_hash BLOB PRIMARY KEY,
 job_id TEXT NOT NULL UNIQUE REFERENCES bundles(job_id) ON DELETE CASCADE,
 owner_id TEXT NOT NULL REFERENCES api_keys(id),
 created_at INTEGER NOT NULL,
 expires_at INTEGER NOT NULL
) STRICT;
CREATE INDEX bundle_tokens_expires_at ON bundle_tokens(expires_at);

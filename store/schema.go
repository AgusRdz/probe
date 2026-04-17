package store

const createSQL = `
CREATE TABLE IF NOT EXISTS endpoints (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    method          TEXT    NOT NULL,
    path_pattern    TEXT    NOT NULL,
    protocol        TEXT    NOT NULL DEFAULT 'rest',
    source          TEXT    NOT NULL DEFAULT 'observed',
    framework       TEXT    NOT NULL DEFAULT '',
    source_file     TEXT    NOT NULL DEFAULT '',
    source_line     INTEGER NOT NULL DEFAULT 0,
    first_seen      TEXT    NOT NULL,
    last_seen       TEXT    NOT NULL,
    call_count      INTEGER NOT NULL DEFAULT 0,
    description     TEXT    NOT NULL DEFAULT '',
    tags_json       TEXT    NOT NULL DEFAULT '[]',
    deprecated      INTEGER NOT NULL DEFAULT 0,
    UNIQUE(method, path_pattern)
);

CREATE TABLE IF NOT EXISTS raw_paths (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    endpoint_id     INTEGER NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    raw_path        TEXT    NOT NULL,
    seen_count      INTEGER NOT NULL DEFAULT 1,
    UNIQUE(endpoint_id, raw_path)
);

CREATE TABLE IF NOT EXISTS path_overrides (
    method           TEXT NOT NULL,
    raw_prefix       TEXT NOT NULL,
    override_pattern TEXT NOT NULL,
    PRIMARY KEY (method, raw_prefix)
);

CREATE TABLE IF NOT EXISTS observations (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    endpoint_id      INTEGER NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    observed_at      TEXT    NOT NULL,
    status_code      INTEGER NOT NULL,
    req_schema_json  TEXT    NOT NULL DEFAULT '',
    resp_schema_json TEXT    NOT NULL DEFAULT '',
    req_content_type  TEXT   NOT NULL DEFAULT '',
    resp_content_type TEXT   NOT NULL DEFAULT '',
    latency_ms       INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS field_confidence (
    endpoint_id INTEGER NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    location    TEXT    NOT NULL,
    field_path  TEXT    NOT NULL,
    seen_count  INTEGER NOT NULL DEFAULT 0,
    total_calls INTEGER NOT NULL DEFAULT 0,
    type_json   TEXT    NOT NULL DEFAULT '{}',
    PRIMARY KEY (endpoint_id, location, field_path)
);

CREATE INDEX IF NOT EXISTS idx_observations_endpoint   ON observations (endpoint_id);
CREATE INDEX IF NOT EXISTS idx_endpoints_method_path   ON endpoints (method, path_pattern);
CREATE INDEX IF NOT EXISTS idx_raw_paths_endpoint      ON raw_paths (endpoint_id);
CREATE INDEX IF NOT EXISTS idx_field_conf_endpoint_loc ON field_confidence (endpoint_id, location);

CREATE TABLE IF NOT EXISTS endpoint_headers (
    endpoint_id  INTEGER NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    header_name  TEXT    NOT NULL,
    seen_count   INTEGER NOT NULL DEFAULT 0,
    total_calls  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (endpoint_id, header_name)
);

CREATE TABLE IF NOT EXISTS query_params (
    endpoint_id  INTEGER NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    param_name   TEXT    NOT NULL,
    seen_count   INTEGER NOT NULL DEFAULT 0,
    total_calls  INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (endpoint_id, param_name)
);

CREATE INDEX IF NOT EXISTS idx_endpoint_headers_ep ON endpoint_headers (endpoint_id);
CREATE INDEX IF NOT EXISTS idx_query_params_ep     ON query_params (endpoint_id);
`

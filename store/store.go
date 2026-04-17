package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/AgusRdz/probe/observer"
	_ "modernc.org/sqlite"
)

// Store wraps a SQLite database connection.
type Store struct {
	db *sql.DB
}

// Endpoint represents a discovered or observed API endpoint.
type Endpoint struct {
	ID          int64
	Method      string
	PathPattern string
	Protocol    string
	Source      string // "scan" | "observed" | "scan+obs"
	Framework   string
	SourceFile  string
	SourceLine  int
	FirstSeen   time.Time
	LastSeen    time.Time
	CallCount   int
	Description string
	Tags        []string
	Deprecated  bool
	RequiresAuth bool
}

// Observation represents a single captured request/response pair.
type Observation struct {
	ID              int64
	EndpointID      int64
	ObservedAt      time.Time
	StatusCode      int
	ReqSchemaJSON   string
	RespSchemaJSON  string
	ReqContentType  string
	RespContentType string
	LatencyMs       int64
}

// FieldConfidenceRow holds aggregated confidence data for a single field.
type FieldConfidenceRow struct {
	EndpointID int64
	Location   string // "request" | "response"
	FieldPath  string // dot-notation e.g. "user.address.city"
	SeenCount  int
	TotalCalls int
	TypeJSON   string // raw JSON of observer.Schema
}

// HeaderRow holds aggregated data for a single observed request header.
type HeaderRow struct {
	EndpointID int64
	HeaderName string
	SeenCount  int
	TotalCalls int
}

// QueryParamRow holds aggregated data for a single observed query parameter.
type QueryParamRow struct {
	EndpointID int64
	ParamName  string
	SeenCount  int
	TotalCalls int
}

// Open opens (or creates) a SQLite DB at path, applies PRAGMAs, and runs the schema DDL.
// If path is empty, the platform default location is used.
func Open(path string) (*Store, error) {
	if path == "" {
		var err error
		path, err = defaultDBPath()
		if err != nil {
			return nil, fmt.Errorf("store: resolve default path: %w", err)
		}
	}

	// URI paths (file:... or :memory:) are passed directly to the driver.
	// Only plain file paths need directory creation and permission enforcement.
	isURI := path == ":memory:" || strings.HasPrefix(path, "file:")
	if !isURI {
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			return nil, fmt.Errorf("store: create directories: %w", err)
		}
		// Create the file with 0600 permissions before the driver opens it.
		if _, err := os.Stat(path); os.IsNotExist(err) {
			f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
			if err != nil {
				return nil, fmt.Errorf("store: create db file: %w", err)
			}
			f.Close()
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open: %w", err)
	}

	// Serialize all writes through a single connection.
	db.SetMaxOpenConns(1)

	if err := applyPragmas(db); err != nil {
		db.Close()
		return nil, err
	}

	if _, err := db.Exec(createSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: apply schema: %w", err)
	}

	// Run migrations — each statement is best-effort (column may already exist).
	for _, stmt := range migrateSQL {
		db.Exec(stmt) //nolint:errcheck
	}

	return &Store{db: db}, nil
}

// Close closes the underlying database connection.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// UpsertEndpoint inserts or updates an endpoint row. Returns the endpoint ID.
// source must be one of "scan", "observed", or "scan+obs".
func (s *Store) UpsertEndpoint(method, pathPattern, protocol, source string) (int64, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	// INSERT OR IGNORE to avoid clobbering first_seen on subsequent calls.
	_, err := s.db.Exec(`
		INSERT OR IGNORE INTO endpoints
			(method, path_pattern, protocol, source, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?, ?)`,
		method, pathPattern, protocol, source, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("store: upsert endpoint insert: %w", err)
	}

	// Always update last_seen, call_count, and source on the existing row.
	_, err = s.db.Exec(`
		UPDATE endpoints
		SET last_seen  = ?,
		    call_count = call_count + 1,
		    source     = CASE
		                    WHEN source = 'scan' AND ? = 'observed' THEN 'scan+obs'
		                    WHEN source = 'observed' AND ? = 'scan' THEN 'scan+obs'
		                    ELSE source
		                 END
		WHERE method = ? AND path_pattern = ?`,
		now, source, source, method, pathPattern,
	)
	if err != nil {
		return 0, fmt.Errorf("store: upsert endpoint update: %w", err)
	}

	var id int64
	err = s.db.QueryRow(
		`SELECT id FROM endpoints WHERE method = ? AND path_pattern = ?`,
		method, pathPattern,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: upsert endpoint select id: %w", err)
	}

	return id, nil
}

// Record persists a captured pair and updates field_confidence.
// Called exclusively by the async drainer goroutine — never from the proxy path.
func (s *Store) Record(pair observer.CapturedPair, reqSchema, respSchema *observer.Schema) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("store: record begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	now := time.Now().UTC().Format(time.RFC3339)

	// Normalize the path for the endpoint pattern; raw path is preserved in raw_paths.
	pathPattern := observer.NormalizePath(pair.RawPath)

	// Detect protocol from content type.
	protocol := observer.DetectProtocol(pair.ReqContentType, pair.ReqBody)

	// Upsert the endpoint using the normalized path pattern.
	endpointID, err := upsertEndpointTx(tx, pair.Method, pathPattern, protocol, "observed", now)
	if err != nil {
		return err
	}

	// Upsert the raw path observation.
	if _, err := tx.Exec(`
		INSERT INTO raw_paths (endpoint_id, raw_path, seen_count)
		VALUES (?, ?, 1)
		ON CONFLICT(endpoint_id, raw_path) DO UPDATE SET seen_count = seen_count + 1`,
		endpointID, pair.RawPath,
	); err != nil {
		return fmt.Errorf("store: record raw_path: %w", err)
	}

	// Marshal schemas to JSON for storage.
	reqJSON, err := marshalSchema(reqSchema)
	if err != nil {
		return fmt.Errorf("store: marshal req schema: %w", err)
	}
	respJSON, err := marshalSchema(respSchema)
	if err != nil {
		return fmt.Errorf("store: marshal resp schema: %w", err)
	}

	// Compute variant fingerprint and upsert the variant row.
	fingerprint := computeVariantFingerprint(pair, reqSchema)
	variantID, err := upsertVariantTx(tx, endpointID, fingerprint, now)
	if err != nil {
		return err
	}

	// Insert the observation row (with variant_id).
	if _, err := tx.Exec(`
		INSERT INTO observations
			(endpoint_id, observed_at, status_code, req_schema_json, resp_schema_json,
			 req_content_type, resp_content_type, latency_ms, variant_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		endpointID, now, pair.StatusCode, reqJSON, respJSON,
		pair.ReqContentType, pair.RespContentType, pair.LatencyMs, variantID,
	); err != nil {
		return fmt.Errorf("store: record observation: %w", err)
	}

	// Update field confidence for request and response schemas (endpoint-level, unchanged).
	if err := updateFieldConfidenceTx(tx, endpointID, "request", reqSchema); err != nil {
		return err
	}
	if err := updateFieldConfidenceTx(tx, endpointID, "response", respSchema); err != nil {
		return err
	}

	// Update variant-scoped field confidence.
	if err := updateVariantFieldConfidenceTx(tx, variantID, "request", reqSchema); err != nil {
		return err
	}
	if err := updateVariantFieldConfidenceTx(tx, variantID, "response", respSchema); err != nil {
		return err
	}

	if err := updateHeadersTx(tx, endpointID, pair.ReqHeaders); err != nil {
		return err
	}
	if err := updateQueryParamsTx(tx, endpointID, pair.RawPath); err != nil {
		return err
	}

	return tx.Commit()
}

// GetEndpoints returns all endpoints ordered alphabetically by path then method.
func (s *Store) GetEndpoints() ([]Endpoint, error) {
	rows, err := s.db.Query(`
		SELECT id, method, path_pattern, protocol, source, framework,
		       source_file, source_line, first_seen, last_seen,
		       call_count, description, tags_json, deprecated, requires_auth
		FROM endpoints
		ORDER BY path_pattern, method`)
	if err != nil {
		return nil, fmt.Errorf("store: get endpoints: %w", err)
	}
	defer rows.Close()

	return scanEndpoints(rows)
}

// GetEndpointByID returns the endpoint with the given ID, or nil if not found.
func (s *Store) GetEndpointByID(id int64) (*Endpoint, error) {
	rows, err := s.db.Query(`
		SELECT id, method, path_pattern, protocol, source, framework,
		       source_file, source_line, first_seen, last_seen,
		       call_count, description, tags_json, deprecated, requires_auth
		FROM endpoints
		WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("store: get endpoint by id: %w", err)
	}
	defer rows.Close()

	endpoints, err := scanEndpoints(rows)
	if err != nil {
		return nil, err
	}
	if len(endpoints) == 0 {
		return nil, nil
	}
	return &endpoints[0], nil
}

// GetFieldConfidence returns all field_confidence rows for an endpoint.
func (s *Store) GetFieldConfidence(endpointID int64) ([]FieldConfidenceRow, error) {
	rows, err := s.db.Query(`
		SELECT endpoint_id, location, field_path, seen_count, total_calls, type_json
		FROM field_confidence
		WHERE endpoint_id = ?
		ORDER BY location, field_path`, endpointID)
	if err != nil {
		return nil, fmt.Errorf("store: get field confidence: %w", err)
	}
	defer rows.Close()

	var result []FieldConfidenceRow
	for rows.Next() {
		var r FieldConfidenceRow
		if err := rows.Scan(&r.EndpointID, &r.Location, &r.FieldPath,
			&r.SeenCount, &r.TotalCalls, &r.TypeJSON); err != nil {
			return nil, fmt.Errorf("store: scan field confidence row: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// GetEndpointHeaders returns all header rows for an endpoint, ordered by header name.
func (s *Store) GetEndpointHeaders(endpointID int64) ([]HeaderRow, error) {
	rows, err := s.db.Query(`
		SELECT endpoint_id, header_name, seen_count, total_calls
		FROM endpoint_headers
		WHERE endpoint_id = ?
		ORDER BY header_name`, endpointID)
	if err != nil {
		return nil, fmt.Errorf("store: get endpoint headers: %w", err)
	}
	defer rows.Close()

	var result []HeaderRow
	for rows.Next() {
		var r HeaderRow
		if err := rows.Scan(&r.EndpointID, &r.HeaderName, &r.SeenCount, &r.TotalCalls); err != nil {
			return nil, fmt.Errorf("store: scan header row: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// GetQueryParams returns all query param rows for an endpoint, ordered by param name.
func (s *Store) GetQueryParams(endpointID int64) ([]QueryParamRow, error) {
	rows, err := s.db.Query(`
		SELECT endpoint_id, param_name, seen_count, total_calls
		FROM query_params
		WHERE endpoint_id = ?
		ORDER BY param_name`, endpointID)
	if err != nil {
		return nil, fmt.Errorf("store: get query params: %w", err)
	}
	defer rows.Close()

	var result []QueryParamRow
	for rows.Next() {
		var r QueryParamRow
		if err := rows.Scan(&r.EndpointID, &r.ParamName, &r.SeenCount, &r.TotalCalls); err != nil {
			return nil, fmt.Errorf("store: scan query param row: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// GetObservations returns up to limit observations for an endpoint, newest first.
func (s *Store) GetObservations(endpointID int64, limit int) ([]Observation, error) {
	rows, err := s.db.Query(`
		SELECT id, endpoint_id, observed_at, status_code,
		       req_schema_json, resp_schema_json,
		       req_content_type, resp_content_type, latency_ms
		FROM observations
		WHERE endpoint_id = ?
		ORDER BY observed_at DESC
		LIMIT ?`, endpointID, limit)
	if err != nil {
		return nil, fmt.Errorf("store: get observations: %w", err)
	}
	defer rows.Close()

	var result []Observation
	for rows.Next() {
		var o Observation
		var observedAt string

		if err := rows.Scan(&o.ID, &o.EndpointID, &observedAt, &o.StatusCode,
			&o.ReqSchemaJSON, &o.RespSchemaJSON,
			&o.ReqContentType, &o.RespContentType, &o.LatencyMs); err != nil {
			return nil, fmt.Errorf("store: scan observation row: %w", err)
		}

		o.ObservedAt, _ = time.Parse(time.RFC3339, observedAt)
		result = append(result, o)
	}
	return result, rows.Err()
}

// DeleteEndpoint removes an endpoint and all cascaded rows.
func (s *Store) DeleteEndpoint(id int64) error {
	_, err := s.db.Exec(`DELETE FROM endpoints WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("store: delete endpoint: %w", err)
	}
	return nil
}

// DeleteAll removes all data from all tables.
func (s *Store) DeleteAll() error {
	tables := []string{"field_confidence", "observations", "raw_paths", "path_overrides", "endpoints"}
	for _, t := range tables {
		if _, err := s.db.Exec(`DELETE FROM ` + t); err != nil {
			return fmt.Errorf("store: delete all from %s: %w", t, err)
		}
	}
	return nil
}

// UpdateEndpointAnnotation sets description and tags on an endpoint.
// Pass empty string for description to leave it unchanged.
// Pass nil for tags to leave them unchanged.
func (s *Store) UpdateEndpointAnnotation(id int64, description string, tags []string) error {
	if description != "" {
		if _, err := s.db.Exec(
			`UPDATE endpoints SET description = ? WHERE id = ?`,
			description, id,
		); err != nil {
			return fmt.Errorf("store: update description: %w", err)
		}
	}

	if tags != nil {
		tagsJSON, err := json.Marshal(tags)
		if err != nil {
			return fmt.Errorf("store: marshal tags: %w", err)
		}
		if _, err := s.db.Exec(
			`UPDATE endpoints SET tags_json = ? WHERE id = ?`,
			string(tagsJSON), id,
		); err != nil {
			return fmt.Errorf("store: update tags: %w", err)
		}
	}

	return nil
}

// UpsertPathOverride inserts or replaces a path override for a raw prefix.
func (s *Store) UpsertPathOverride(method, rawPrefix, overridePattern string) error {
	if _, err := s.db.Exec(`
		INSERT INTO path_overrides (method, raw_prefix, override_pattern)
		VALUES (?, ?, ?)
		ON CONFLICT(method, raw_prefix) DO UPDATE SET override_pattern = excluded.override_pattern`,
		method, rawPrefix, overridePattern,
	); err != nil {
		return fmt.Errorf("store: upsert path override: %w", err)
	}
	return nil
}

// Stats returns endpoint counts by source.
// Keys: "total", "observed", "scan", "scan+obs".
func (s *Store) Stats() (map[string]int, error) {
	result := map[string]int{
		"total":    0,
		"observed": 0,
		"scan":     0,
		"scan+obs": 0,
	}

	rows, err := s.db.Query(`SELECT source, COUNT(*) FROM endpoints GROUP BY source`)
	if err != nil {
		return nil, fmt.Errorf("store: stats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var source string
		var count int
		if err := rows.Scan(&source, &count); err != nil {
			return nil, fmt.Errorf("store: scan stats row: %w", err)
		}
		result[source] = count
		result["total"] += count
	}
	return result, rows.Err()
}

// ScannedEndpointInput carries the fields from a static-analysis scan result.
// Using a dedicated input type avoids a circular import between store and scanner.
type ScannedEndpointInput struct {
	Method      string
	PathPattern string
	Protocol    string
	Framework   string
	SourceFile  string
	SourceLine  int
	ReqSchema   *observer.Schema
	RespSchema  *observer.Schema
	StatusCodes []int
	Description string
	Tags        []string
	Deprecated  bool
	RequiresAuth bool
}

// UpsertScannedEndpoint stores a ScannedEndpoint discovered by probe scan.
// If the endpoint already exists as "observed", upgrades source to "scan+obs".
// Stores req/resp schema from scan as initial field_confidence skeleton rows
// (seen_count=0, total_calls=0) that traffic observation will later increment.
// Returns true if the endpoint row was newly inserted, false if it already existed.
func (s *Store) UpsertScannedEndpoint(input ScannedEndpointInput) (bool, error) {
	now := time.Now().UTC().Format(time.RFC3339)

	res, err := s.db.Exec(`
		INSERT OR IGNORE INTO endpoints
			(method, path_pattern, protocol, source, first_seen, last_seen)
		VALUES (?, ?, ?, 'scan', ?, ?)`,
		input.Method, input.PathPattern, input.Protocol, now, now,
	)
	if err != nil {
		return false, fmt.Errorf("store: upsert scanned endpoint insert: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("store: upsert scanned endpoint rows affected: %w", err)
	}
	isNew := rowsAffected > 0

	deprecated := 0
	if input.Deprecated {
		deprecated = 1
	}

	requiresAuth := 0
	if input.RequiresAuth {
		requiresAuth = 1
	}

	tagsJSON, err := json.Marshal(input.Tags)
	if err != nil {
		return false, fmt.Errorf("store: marshal tags: %w", err)
	}

	_, err = s.db.Exec(`
		UPDATE endpoints
		SET source      = CASE
		                     WHEN source = 'observed' THEN 'scan+obs'
		                     ELSE source
		                  END,
		    framework   = ?,
		    source_file = ?,
		    source_line = ?,
		    description = ?,
		    deprecated  = ?,
		    requires_auth = ?,
		    tags_json   = ?,
		    last_seen   = ?
		WHERE method = ? AND path_pattern = ?`,
		input.Framework, input.SourceFile, input.SourceLine,
		input.Description, deprecated, requiresAuth, string(tagsJSON), now,
		input.Method, input.PathPattern,
	)
	if err != nil {
		return false, fmt.Errorf("store: upsert scanned endpoint update: %w", err)
	}

	var endpointID int64
	err = s.db.QueryRow(
		`SELECT id FROM endpoints WHERE method = ? AND path_pattern = ?`,
		input.Method, input.PathPattern,
	).Scan(&endpointID)
	if err != nil {
		return false, fmt.Errorf("store: upsert scanned endpoint select id: %w", err)
	}

	// Insert skeleton field_confidence rows (seen_count=0, total_calls=0).
	if err := insertSkeletonFieldsTx(s.db, endpointID, "request", input.ReqSchema); err != nil {
		return false, err
	}
	if err := insertSkeletonFieldsTx(s.db, endpointID, "response", input.RespSchema); err != nil {
		return false, err
	}

	return isNew, nil
}

// insertSkeletonFieldsTx inserts field_confidence rows for every leaf in schema
// with seen_count=0 and total_calls=0. Uses INSERT OR IGNORE so existing rows
// (already populated by traffic observation) are not overwritten.
func insertSkeletonFieldsTx(db *sql.DB, endpointID int64, location string, schema *observer.Schema) error {
	if schema == nil {
		return nil
	}

	fields := map[string]*observer.Schema{}
	flattenSchema(schema, "", fields)

	for fieldPath, fieldSchema := range fields {
		typeJSON, err := json.Marshal(fieldSchema)
		if err != nil {
			return fmt.Errorf("store: marshal skeleton field schema %q: %w", fieldPath, err)
		}
		if _, err := db.Exec(`
			INSERT OR IGNORE INTO field_confidence
				(endpoint_id, location, field_path, seen_count, total_calls, type_json)
			VALUES (?, ?, ?, 0, 0, ?)`,
			endpointID, location, fieldPath, string(typeJSON),
		); err != nil {
			return fmt.Errorf("store: insert skeleton field_confidence %q: %w", fieldPath, err)
		}
	}
	return nil
}

// --- private helpers ---

func applyPragmas(db *sql.DB) error {
	pragmas := []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA synchronous=NORMAL`,
		`PRAGMA foreign_keys=ON`,
		`PRAGMA busy_timeout=5000`,
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("store: pragma %q: %w", p, err)
		}
	}
	return nil
}

func defaultDBPath() (string, error) {
	return dbPathForName("probe")
}

// DBPathForDir returns a DB path derived from the scan directory name,
// e.g. scanning "ciranet-identity-api" → .../ciranet-identity-api.db
// Falls back to the default probe.db if the directory name is empty.
func DBPathForDir(dir string) (string, error) {
	name := filepath.Base(filepath.Clean(dir))
	if name == "" || name == "." {
		return defaultDBPath()
	}
	// Sanitise: keep only filename-safe chars.
	var b strings.Builder
	for _, c := range strings.ToLower(name) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			b.WriteRune(c)
		} else {
			b.WriteRune('-')
		}
	}
	safe := strings.Trim(b.String(), "-")
	if safe == "" {
		return defaultDBPath()
	}
	return dbPathForName(safe)
}

// DBPathForTarget returns the DB path derived from the target URL's hostname,
// e.g. https://resident-api.local.ciranet.com:443 → .../resident-api.local.ciranet.com.db
// Falls back to the default probe.db if the URL cannot be parsed.
func DBPathForTarget(target string) (string, error) {
	name := hostnameFromURL(target)
	if name == "" {
		return defaultDBPath()
	}
	return dbPathForName(name)
}

func hostnameFromURL(raw string) string {
	// Minimal parse — avoid importing net/url just for this.
	s := raw
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	// Strip port.
	if i := strings.LastIndexByte(s, ':'); i >= 0 {
		s = s[:i]
	}
	// Sanitise: keep only hostname-safe chars.
	var b strings.Builder
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-' || c == '.' {
			b.WriteRune(c)
		}
	}
	return strings.ToLower(b.String())
}

func dbPathForName(name string) (string, error) {
	var base string
	if runtime.GOOS == "windows" {
		base = os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", fmt.Errorf("%%LOCALAPPDATA%% is not set")
		}
		return filepath.Join(base, "probe", name+".db"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "probe", name+".db"), nil
}

// upsertEndpointTx is the transactional variant of UpsertEndpoint.
func upsertEndpointTx(tx *sql.Tx, method, pathPattern, protocol, source, now string) (int64, error) {
	_, err := tx.Exec(`
		INSERT OR IGNORE INTO endpoints
			(method, path_pattern, protocol, source, first_seen, last_seen)
		VALUES (?, ?, ?, ?, ?, ?)`,
		method, pathPattern, protocol, source, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("store: upsert endpoint tx insert: %w", err)
	}

	_, err = tx.Exec(`
		UPDATE endpoints
		SET last_seen  = ?,
		    call_count = call_count + 1,
		    source     = CASE
		                    WHEN source = 'scan' AND ? = 'observed' THEN 'scan+obs'
		                    WHEN source = 'observed' AND ? = 'scan' THEN 'scan+obs'
		                    ELSE source
		                 END
		WHERE method = ? AND path_pattern = ?`,
		now, source, source, method, pathPattern,
	)
	if err != nil {
		return 0, fmt.Errorf("store: upsert endpoint tx update: %w", err)
	}

	var id int64
	err = tx.QueryRow(
		`SELECT id FROM endpoints WHERE method = ? AND path_pattern = ?`,
		method, pathPattern,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("store: upsert endpoint tx select id: %w", err)
	}

	return id, nil
}

// updateFieldConfidenceTx upserts field_confidence rows for every leaf in schema.
// Fields present in this call: seen_count+1, total_calls+1.
// Fields previously seen but absent this call: total_calls+1 only.
func updateFieldConfidenceTx(tx *sql.Tx, endpointID int64, location string, schema *observer.Schema) error {
	if schema == nil {
		// Still need to increment total_calls for all existing fields.
		return bumpTotalCallsTx(tx, endpointID, location, nil)
	}

	present := map[string]*observer.Schema{}
	flattenSchema(schema, "", present)

	// Upsert present fields.
	for fieldPath, fieldSchema := range present {
		typeJSON, err := json.Marshal(fieldSchema)
		if err != nil {
			return fmt.Errorf("store: marshal field schema %q: %w", fieldPath, err)
		}

		if _, err := tx.Exec(`
			INSERT INTO field_confidence (endpoint_id, location, field_path, seen_count, total_calls, type_json)
			VALUES (?, ?, ?, 1, 1, ?)
			ON CONFLICT(endpoint_id, location, field_path)
			DO UPDATE SET
				seen_count  = seen_count  + 1,
				total_calls = total_calls + 1,
				type_json   = excluded.type_json`,
			endpointID, location, fieldPath, string(typeJSON),
		); err != nil {
			return fmt.Errorf("store: upsert field_confidence %q: %w", fieldPath, err)
		}
	}

	// Increment total_calls for existing fields NOT present in this observation.
	return bumpTotalCallsTx(tx, endpointID, location, present)
}

// bumpTotalCallsTx increments total_calls for all rows in (endpoint, location)
// whose field_path is NOT in the present set.
func bumpTotalCallsTx(tx *sql.Tx, endpointID int64, location string, present map[string]*observer.Schema) error {
	rows, err := tx.Query(`
		SELECT field_path FROM field_confidence
		WHERE endpoint_id = ? AND location = ?`,
		endpointID, location,
	)
	if err != nil {
		return fmt.Errorf("store: bump total_calls query: %w", err)
	}
	defer rows.Close()

	var absent []string
	for rows.Next() {
		var fp string
		if err := rows.Scan(&fp); err != nil {
			return fmt.Errorf("store: bump total_calls scan: %w", err)
		}
		if _, seen := present[fp]; !seen {
			absent = append(absent, fp)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	for _, fp := range absent {
		if _, err := tx.Exec(`
			UPDATE field_confidence
			SET total_calls = total_calls + 1
			WHERE endpoint_id = ? AND location = ? AND field_path = ?`,
			endpointID, location, fp,
		); err != nil {
			return fmt.Errorf("store: bump total_calls update %q: %w", fp, err)
		}
	}
	return nil
}

// flattenSchema recursively walks a Schema and records every leaf field.
// Object properties are recursed with dot-notation prefixes.
// Arrays use the parent path (array items are not individually keyed).
func flattenSchema(s *observer.Schema, prefix string, out map[string]*observer.Schema) {
	if s == nil {
		return
	}
	if len(s.Properties) == 0 {
		// Leaf node.
		if prefix != "" {
			out[prefix] = s
		}
		return
	}
	for name, child := range s.Properties {
		key := name
		if prefix != "" {
			key = prefix + "." + name
		}
		flattenSchema(child, key, out)
	}
}

// marshalSchema encodes a Schema pointer to a JSON string, or "" for nil.
// The observations columns are NOT NULL so we must never return SQL NULL.
func marshalSchema(s *observer.Schema) (string, error) {
	if s == nil {
		return "", nil
	}
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// updateHeadersTx upserts endpoint_headers rows for the headers observed in this request.
// Headers present: seen_count+1, total_calls+1.
// Headers previously seen but absent: total_calls+1 only.
func updateHeadersTx(tx *sql.Tx, endpointID int64, headers []string) error {
	present := make(map[string]bool, len(headers))
	for _, h := range headers {
		present[h] = true
		if _, err := tx.Exec(`
			INSERT INTO endpoint_headers (endpoint_id, header_name, seen_count, total_calls)
			VALUES (?, ?, 1, 1)
			ON CONFLICT(endpoint_id, header_name) DO UPDATE SET
				seen_count  = seen_count  + 1,
				total_calls = total_calls + 1`,
			endpointID, h,
		); err != nil {
			return fmt.Errorf("store: upsert endpoint_header %q: %w", h, err)
		}
	}

	// Bump total_calls for headers seen before but not in this request.
	rows, err := tx.Query(
		`SELECT header_name FROM endpoint_headers WHERE endpoint_id = ?`, endpointID)
	if err != nil {
		return fmt.Errorf("store: query existing headers: %w", err)
	}
	defer rows.Close()
	var absent []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("store: scan header name: %w", err)
		}
		if !present[name] {
			absent = append(absent, name)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	for _, name := range absent {
		if _, err := tx.Exec(`
			UPDATE endpoint_headers SET total_calls = total_calls + 1
			WHERE endpoint_id = ? AND header_name = ?`,
			endpointID, name,
		); err != nil {
			return fmt.Errorf("store: bump header total_calls %q: %w", name, err)
		}
	}
	return nil
}

// updateQueryParamsTx upserts query_params rows for params observed in this raw path.
// rawPath is the full RequestURI (e.g. "/api/users?page=1&limit=10").
func updateQueryParamsTx(tx *sql.Tx, endpointID int64, rawPath string) error {
	// Extract query string.
	query := ""
	if idx := strings.IndexByte(rawPath, '?'); idx >= 0 {
		query = rawPath[idx+1:]
	}
	if query == "" {
		// No query params in this request — bump total_calls for any previously seen.
		rows, err := tx.Query(
			`SELECT param_name FROM query_params WHERE endpoint_id = ?`, endpointID)
		if err != nil {
			return fmt.Errorf("store: query existing query params: %w", err)
		}
		defer rows.Close()
		var names []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				return fmt.Errorf("store: scan param name: %w", err)
			}
			names = append(names, name)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		rows.Close()
		for _, name := range names {
			if _, err := tx.Exec(`
				UPDATE query_params SET total_calls = total_calls + 1
				WHERE endpoint_id = ? AND param_name = ?`,
				endpointID, name,
			); err != nil {
				return fmt.Errorf("store: bump query param total_calls %q: %w", name, err)
			}
		}
		return nil
	}

	// Parse param names (values discarded — never stored).
	present := map[string]bool{}
	for _, part := range strings.Split(query, "&") {
		if part == "" {
			continue
		}
		name := part
		if idx := strings.IndexByte(part, '='); idx >= 0 {
			name = part[:idx]
		}
		if name == "" {
			continue
		}
		present[name] = true
	}

	for name := range present {
		if _, err := tx.Exec(`
			INSERT INTO query_params (endpoint_id, param_name, seen_count, total_calls)
			VALUES (?, ?, 1, 1)
			ON CONFLICT(endpoint_id, param_name) DO UPDATE SET
				seen_count  = seen_count  + 1,
				total_calls = total_calls + 1`,
			endpointID, name,
		); err != nil {
			return fmt.Errorf("store: upsert query_param %q: %w", name, err)
		}
	}

	// Bump total_calls for previously seen params absent from this request.
	rows, err := tx.Query(
		`SELECT param_name FROM query_params WHERE endpoint_id = ?`, endpointID)
	if err != nil {
		return fmt.Errorf("store: query existing query params: %w", err)
	}
	defer rows.Close()
	var absent []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return fmt.Errorf("store: scan param name: %w", err)
		}
		if !present[name] {
			absent = append(absent, name)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()

	for _, name := range absent {
		if _, err := tx.Exec(`
			UPDATE query_params SET total_calls = total_calls + 1
			WHERE endpoint_id = ? AND param_name = ?`,
			endpointID, name,
		); err != nil {
			return fmt.Errorf("store: bump query param total_calls %q: %w", name, err)
		}
	}
	return nil
}

// scanEndpoints reads endpoint rows from a *sql.Rows result set.
func scanEndpoints(rows *sql.Rows) ([]Endpoint, error) {
	var result []Endpoint
	for rows.Next() {
		var e Endpoint
		var firstSeen, lastSeen string
		var tagsJSON string
		var deprecated int
		var requiresAuth int

		if err := rows.Scan(
			&e.ID, &e.Method, &e.PathPattern, &e.Protocol,
			&e.Source, &e.Framework, &e.SourceFile, &e.SourceLine,
			&firstSeen, &lastSeen, &e.CallCount,
			&e.Description, &tagsJSON, &deprecated, &requiresAuth,
		); err != nil {
			return nil, fmt.Errorf("store: scan endpoint row: %w", err)
		}

		e.FirstSeen, _ = time.Parse(time.RFC3339, firstSeen)
		e.LastSeen, _ = time.Parse(time.RFC3339, lastSeen)
		e.Deprecated = deprecated != 0
		e.RequiresAuth = requiresAuth != 0

		if err := json.Unmarshal([]byte(tagsJSON), &e.Tags); err != nil {
			e.Tags = []string{}
		}

		result = append(result, e)
	}
	return result, rows.Err()
}

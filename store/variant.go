package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/AgusRdz/probe/observer"
)

// RequestVariant represents a distinct request shape for an endpoint.
// Two observations belong to the same variant when their fingerprints match.
type RequestVariant struct {
	ID          int64
	EndpointID  int64
	Fingerprint string
	Label       string
	FirstSeen   time.Time
	LastSeen    time.Time
	CallCount   int
}

// computeVariantFingerprint builds a stable opaque key from:
//   - auth scheme derived from the Authorization header token in pair.ReqHeaders
//     (header entries may carry a ":<scheme>" suffix set by capture.go, e.g.
//     "Authorization:bearer"; absent or unrecognised → "none")
//   - sorted top-level field names from reqSchema.Properties (nil/non-object → empty)
//
// Only top-level fields are used to avoid false splits from optional nested fields.
func computeVariantFingerprint(pair observer.CapturedPair, reqSchema *observer.Schema) string {
	authScheme := "none"
	for _, h := range pair.ReqHeaders {
		lower := strings.ToLower(h)
		if strings.HasPrefix(lower, "authorization:") {
			scheme := strings.TrimPrefix(lower, "authorization:")
			switch scheme {
			case "bearer":
				authScheme = "bearer"
			case "basic":
				authScheme = "basic"
			case "apikey":
				authScheme = "apikey"
			default:
				authScheme = "other"
			}
			break
		}
		if lower == "authorization" {
			authScheme = "bearer" // default assumption when no scheme suffix
			break
		}
	}

	var fields []string
	if reqSchema != nil && reqSchema.Type == "object" {
		for name := range reqSchema.Properties {
			fields = append(fields, name)
		}
		sort.Strings(fields)
	}

	return "auth:" + authScheme + "|body:" + strings.Join(fields, ",")
}

// upsertVariantTx inserts or updates a request_variants row and returns its ID.
func upsertVariantTx(tx *sql.Tx, endpointID int64, fingerprint, now string) (int64, error) {
	_, err := tx.Exec(`
		INSERT INTO request_variants (endpoint_id, fingerprint, first_seen, last_seen, call_count)
		VALUES (?, ?, ?, ?, 1)
		ON CONFLICT(endpoint_id, fingerprint) DO UPDATE SET
			last_seen  = excluded.last_seen,
			call_count = call_count + 1`,
		endpointID, fingerprint, now, now,
	)
	if err != nil {
		return 0, fmt.Errorf("store: upsert variant: %w", err)
	}

	var id int64
	if err := tx.QueryRow(
		`SELECT id FROM request_variants WHERE endpoint_id = ? AND fingerprint = ?`,
		endpointID, fingerprint,
	).Scan(&id); err != nil {
		return 0, fmt.Errorf("store: select variant id: %w", err)
	}
	return id, nil
}

// updateVariantFieldConfidenceTx mirrors updateFieldConfidenceTx but scoped to a variant.
func updateVariantFieldConfidenceTx(tx *sql.Tx, variantID int64, location string, schema *observer.Schema) error {
	if schema == nil {
		return bumpVariantTotalCallsTx(tx, variantID, location, nil)
	}

	present := map[string]*observer.Schema{}
	flattenSchema(schema, "", present)

	for fieldPath, fieldSchema := range present {
		typeJSON, err := json.Marshal(fieldSchema)
		if err != nil {
			return fmt.Errorf("store: marshal variant field schema %q: %w", fieldPath, err)
		}
		if _, err := tx.Exec(`
			INSERT INTO variant_field_confidence (variant_id, location, field_path, seen_count, total_calls, type_json)
			VALUES (?, ?, ?, 1, 1, ?)
			ON CONFLICT(variant_id, location, field_path) DO UPDATE SET
				seen_count  = seen_count  + 1,
				total_calls = total_calls + 1,
				type_json   = excluded.type_json`,
			variantID, location, fieldPath, string(typeJSON),
		); err != nil {
			return fmt.Errorf("store: upsert variant_field_confidence %q: %w", fieldPath, err)
		}
	}

	return bumpVariantTotalCallsTx(tx, variantID, location, present)
}

// bumpVariantTotalCallsTx increments total_calls for variant fields absent from this observation.
func bumpVariantTotalCallsTx(tx *sql.Tx, variantID int64, location string, present map[string]*observer.Schema) error {
	rows, err := tx.Query(`
		SELECT field_path FROM variant_field_confidence
		WHERE variant_id = ? AND location = ?`,
		variantID, location,
	)
	if err != nil {
		return fmt.Errorf("store: bump variant total_calls query: %w", err)
	}
	defer rows.Close()

	var absent []string
	for rows.Next() {
		var fp string
		if err := rows.Scan(&fp); err != nil {
			return fmt.Errorf("store: bump variant total_calls scan: %w", err)
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
			UPDATE variant_field_confidence
			SET total_calls = total_calls + 1
			WHERE variant_id = ? AND location = ? AND field_path = ?`,
			variantID, location, fp,
		); err != nil {
			return fmt.Errorf("store: bump variant total_calls update %q: %w", fp, err)
		}
	}
	return nil
}

// GetVariants returns all request_variants for an endpoint, ordered by first_seen.
func (s *Store) GetVariants(endpointID int64) ([]RequestVariant, error) {
	rows, err := s.db.Query(`
		SELECT id, endpoint_id, fingerprint, label, first_seen, last_seen, call_count
		FROM request_variants
		WHERE endpoint_id = ?
		ORDER BY first_seen`, endpointID)
	if err != nil {
		return nil, fmt.Errorf("store: get variants: %w", err)
	}
	defer rows.Close()

	var result []RequestVariant
	for rows.Next() {
		var v RequestVariant
		var firstSeen, lastSeen string
		if err := rows.Scan(&v.ID, &v.EndpointID, &v.Fingerprint, &v.Label,
			&firstSeen, &lastSeen, &v.CallCount); err != nil {
			return nil, fmt.Errorf("store: scan variant row: %w", err)
		}
		v.FirstSeen, _ = time.Parse(time.RFC3339, firstSeen)
		v.LastSeen, _ = time.Parse(time.RFC3339, lastSeen)
		result = append(result, v)
	}
	return result, rows.Err()
}

// GetVariantFieldConfidence returns variant_field_confidence rows for a variant.
func (s *Store) GetVariantFieldConfidence(variantID int64) ([]FieldConfidenceRow, error) {
	rows, err := s.db.Query(`
		SELECT variant_id, location, field_path, seen_count, total_calls, type_json
		FROM variant_field_confidence
		WHERE variant_id = ?
		ORDER BY location, field_path`, variantID)
	if err != nil {
		return nil, fmt.Errorf("store: get variant field confidence: %w", err)
	}
	defer rows.Close()

	var result []FieldConfidenceRow
	for rows.Next() {
		var r FieldConfidenceRow
		if err := rows.Scan(&r.EndpointID, &r.Location, &r.FieldPath,
			&r.SeenCount, &r.TotalCalls, &r.TypeJSON); err != nil {
			return nil, fmt.Errorf("store: scan variant field confidence row: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

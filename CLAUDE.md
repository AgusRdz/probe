# probe — CLAUDE.md

Agent guide and architecture invariants. Read this before touching any code.

---

## What probe is

A transparent reverse proxy that discovers and documents API endpoints by observing real traffic.
No code changes to target services. No OpenAPI spec required. Pure observation.

**Pairs with:** `spec` (mock server). Workflow: `probe intercept` → `probe export` → `spec serve`.

---

## Critical invariants — never break these

### Proxy
1. **Traffic is never modified.** `httputil.ReverseProxy` is used as-is. `capture.Wrap()` only reads — never alters request or response content, headers, or status codes.
2. **Body is read once via `io.TeeReader`.** The reader flows to the target AND to the schema inferrer simultaneously. Never buffer the full body before forwarding.
3. **DB writes are async.** A buffered channel (size 100) decouples proxy response from DB write. The proxy goroutine sends and returns immediately. A separate drainer goroutine writes to SQLite. This keeps proxy latency < 5ms.
4. **`--target` is validated at startup.** Must be `http://` or `https://`. A HEAD request is issued on startup — fail fast if unreachable.
5. **Default bind is `127.0.0.1`, never `0.0.0.0`.** `probe` is a local dev tool. Binding to 0.0.0.0 is a foot-gun. Require explicit `--bind 0.0.0.0` to expose.

### Storage / privacy
6. **Field values are NEVER stored.** `store.Record()` accepts a `Schema` (type + format + presence), not a value. Raw request/response payloads are never persisted.
7. **Raw path observations stored first, patterns computed on read.** Never normalize paths during capture — store the raw observed path. Recompute patterns during `probe list` / `probe export` / `probe show`. This allows retroactive re-normalization when new evidence changes the pattern.

### Schema inference
8. **Inference is per-protocol.** The `observer` package dispatches on `Content-Type` to the appropriate schema extractor. Unrecognized content types are recorded as `{type: "string", format: "binary"}` — never silently dropped.
9. **Field confidence is cumulative across all observations.** `seen_count / total_calls` per field, per location (request/response), per endpoint. Never reset on new observations — only accumulate.
10. **Path normalization is probabilistic, not deterministic.** A segment is only marked `{id}` when N confirmations agree (configurable, default 3). Manual overrides in `.probe.yml` always win.

### Output
11. **Color is TTY-gated.** Use `isTTY()` check. `--no-color` always wins. Never output ANSI escapes to a pipe.
12. **Non-JSON, non-parseable responses are recorded, never dropped.** Record `content_type`, `status_code`, and `latency_ms` even when body cannot be parsed. Body schema stays null.

---

## Package responsibilities

| Package | Does | Does NOT |
|---------|------|----------|
| `scanner/` | Read source files, detect framework, extract routes + schemas | Execute code, modify files, write to scanned dirs |
| `proxy/` | Forward requests, capture req/resp pairs via TeeReader | Modify traffic, parse schemas |
| `capture/` | Read body once, dispatch to observer, send to store channel | Store directly, normalize paths |
| `observer/` | Infer schemas, normalize paths (probabilistic), score confidence | Proxy, store, render |
| `store/` | SQLite CRUD, WAL mode, migration | Inference, rendering |
| `export/` | Query store, assemble OpenAPI / Postman output | Proxy, inference |
| `render/` | Table, detail, JSON output to stdout | Anything else |
| `config/` | Load `~/.config/probe/config.yml` + `.probe.yml` | Anything else |
| `updater/` | Self-update with Ed25519 verification | Anything else |

---

## Scanner invariants — never break these

13. **Scanners are read-only.** They walk files and match patterns. They never execute code, never shell out, never write to scanned directories. If a framework requires code execution to resolve routes (e.g., Rails `rake routes`), produce skeleton-only output instead.
14. **Scanners use regex + `go/ast`, never language runtimes.** Route extraction is text-matching against source files. For Go projects, use `go/ast` (stdlib). For all other languages, `regexp` + `bufio.Scanner`. No tree-sitter, no external parsers, no language-specific tools.
15. **Scan results never overwrite observed schemas.** When a scanned endpoint already exists in the store with `source=observed`, merging a scan result only adds missing fields from the scan — it never reduces confidence or replaces observed field types.
16. **Scan stores `source_file` and `source_line`.** Every scanned endpoint must record where it came from so `probe show` can display it and users can navigate to the source.
17. **Framework detection is best-effort, never fatal.** If detection produces multiple candidates or none, surface them to the user and let `--framework` override. Never abort because detection was ambiguous.
18. **Extractor failures are isolated.** If the NestJS extractor panics or errors on a file, log it to stderr and continue. One bad file should never abort the whole scan.
19. **`deprecated` field is propagated.** If a route is annotated `@Deprecated` (Java), `[Obsolete]` (C#), or `@deprecated` (JSDoc), set `deprecated: true` in the stored endpoint and in the exported OpenAPI spec.

## Multi-protocol support

`capture.go` dispatches schema extraction based on `Content-Type`:

```
application/json                → observer.InferJSONSchema()
application/graphql             → observer.InferGraphQLSchema()
application/x-www-form-urlencoded → observer.InferFormSchema()
multipart/form-data             → observer.InferMultipartSchema()
application/xml, text/xml       → observer.InferXMLSchema()
application/grpc                → observer.InferGRPCSchema()   (via reflection)
application/x-protobuf          → record as binary, mark protocol: "grpc"
*/* or unrecognized             → record as binary schema
```

### GraphQL specifics
- Detect: `POST` to any path where request body contains `{"query": "..."}` or `{"operationName": "..."}`
- Extract operation type (`query` / `mutation` / `subscription`) from the `query` field
- Store operation name as a virtual "path": `/graphql#OperationName`
- Response: extract `data` key only (skip `errors` — they vary)
- Do NOT try to parse the GraphQL SDL from the query string — only infer from JSON response shapes

### gRPC specifics
- Detect via `Content-Type: application/grpc` or `application/grpc+proto`
- Attempt gRPC server reflection (`grpc.reflection.v1alpha`) on startup if `--grpc-reflect` flag is passed
- Without reflection: record method path (`/package.Service/Method`) + status code only; mark schema as `{type: "object", format: "protobuf"}`
- With reflection: use `protodesc` to get message descriptors, infer schema from field definitions
- gRPC-Web and gRPC-transcoding (JSON over HTTP) are handled by the JSON path

### XML specifics
- Parse via `encoding/xml`; convert element tree to schema (same shape as JSON inference)
- Attributes become fields with `xml_attr: true` annotation in schema
- Text content becomes `_text` field
- Max depth: 20 levels (prevent pathological nesting)

### Form-encoded
- Parse via `net/url.ParseQuery`
- All values inferred as `string` (form fields are always strings)
- No nested object inference (flat only)

---

## Path normalization rules (ordered, applied during pattern computation)

```
1. Pure integers (all digits)           → {id}
2. UUID (8-4-4-4-12 hex)               → {id}
3. ULID (26 chars, Crockford base32)   → {id}
4. CUID / NanoID (22+ alphanumeric)    → {id}
5. Slug-with-numeric-suffix (abc-42)   → {id}
6. ALL-CAPS-alphanumeric (ORD-9821)    → {id}
7. Cross-call confirmation: segment seen as integer in ≥N calls, same position → {id}
8. Known semantic keywords (me, self, current, latest, new, first, last, all) → keep as-is
9. Long strings / hash-like (>32 chars) → {id}
10. Manual override in .probe.yml       → wins over all rules
```

**Confidence in normalization** is stored per segment position. A segment must reach `path_normalization_confidence_threshold` (default 3 confirming calls) before it's promoted to `{id}` in exports. `probe list` shows `?` suffix on unconfirmed patterns.

---

## SQLite configuration

- **WAL mode enabled on open.** Allows concurrent readers while a writer is active.
- **Single writer goroutine** draining the channel. All writes serialized — no mutex needed on writes.
- **`PRAGMA journal_mode=WAL`** and **`PRAGMA synchronous=NORMAL`** set on every connection open.
- **Body size limit:** 1MB per request/response body. Truncate at 1MB before schema inference.
- **DB path:** `~/.local/share/probe/probe.db` (Linux/Mac) or `%LOCALAPPDATA%\probe\probe.db` (Windows).

For team/shared use: document that SQLite WAL handles concurrent reads from multiple `probe list` calls fine, but `probe intercept` should be run by one process at a time per DB file. Multiple instances = multiple DB files (use `--db` flag).

---

## Configuration

Two-level config (identical to go-cli-boilerplate pattern):
1. Global: `~/.config/probe/config.yml`
2. Project: `.probe.yml` (walk up from cwd, project overrides global)

```yaml
# .probe.yml example
proxy:
  port: 4000
  target: http://localhost:3001
  filter: /api
  ignore:
    - /health
    - /metrics
  body_size_limit: 1048576       # 1MB
  bind: 127.0.0.1

inference:
  path_normalization_threshold: 3   # calls before segment → {id}
  confidence_threshold: 0.9          # required vs optional field cutoff
  max_xml_depth: 20

export:
  default_format: openapi
  min_confidence: 0.0

path_overrides:
  # Prevent normalization of known non-ID segments
  - pattern: "/api/v*/users/me"
    keep_as: "/api/v{version}/users/me"
  - pattern: "/releases/stable"
    keep_as: "/releases/stable"
```

---

## Testing strategy

```
proxy/
  proxy_test.go         — httptest.Server target; verify response parity (body, headers, status)
  capture_test.go       — capture invoked per request; TeeReader doesn't corrupt body

observer/
  path_test.go          — table-driven: input path → expected pattern + confirmation count
  schema_json_test.go   — JSON value → inferred Schema type/format
  schema_graphql_test.go — GraphQL response → inferred Schema
  schema_xml_test.go    — XML body → inferred Schema
  schema_form_test.go   — form-encoded body → inferred Schema
  confidence_test.go    — field confidence after N observations (required vs optional)

store/
  store_test.go         — upsert endpoints, insert observations, query confidence
                          use real SQLite (in-memory: file::memory:?cache=shared)

export/
  openapi_test.go       — golden: observations fixture → openapi.yaml
  postman_test.go       — golden: observations fixture → collection.json

config/
  config_test.go        — load global, load project override, merge

Integration tests (-tags=integration):
  — Full round trip: proxy → capture → store → export → validate spec
  — Concurrent readers (WAL mode): 10 goroutines reading while writer inserts
  — GraphQL round trip: POST /graphql → extracted operation in list
  — Large body truncation: body > 1MB is truncated before inference
```

**Rules:**
- TDD: failing test first, always
- Real SQLite in tests (`file::memory:`) — never mock the store
- Table-driven for all parsers and normalizers
- Golden files in `testdata/` for export tests
- Run with `-race` flag always

---

## CLI design (stdlib flag, no framework)

Top-level dispatch via `os.Args[1]` switch (same pattern as logr / go-cli-boilerplate).
No external CLI framework. Stdlib `flag.FlagSet` per subcommand.

```
probe intercept [flags]
probe list [flags]
probe show <METHOD> <PATH> [flags]
probe export [flags]
probe annotate <"METHOD /path"> [flags]
probe stats [flags]
probe clear [flags]
probe config [show]
probe update
probe version
probe help [command]
```

---

## Dependencies (keep minimal)

```
github.com/mattn/go-isatty           — TTY detection for color safety
modernc.org/sqlite                   — CGO-free SQLite (cross-platform, no C toolchain)
gopkg.in/yaml.v3                     — Config parsing
google.golang.org/grpc               — gRPC reflection client (only when --grpc-reflect used)
google.golang.org/protobuf           — protodesc for gRPC schema inference
gopkg.in/yaml.v3                     — OpenAPI YAML output
```

gRPC dependencies are only imported in `observer/grpc.go`. If gRPC reflection is not needed, the binary still works — gRPC deps are tree-shaken at link time for non-gRPC use.

Do NOT add: cobra, viper, zerolog, logrus, or any HTTP framework. Keep it minimal.

---

## Build / release

- Docker-based builds (golang:1.24-alpine). Go never required on host.
- Cross-compile: linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, windows/amd64
- Ed25519 signing on release binaries (same as go-cli-boilerplate)
- `git-cliff` for changelog
- GitHub Actions pinned to commit SHAs (not version tags)
- Install scripts: `install.sh` (Unix) + `install.ps1` (Windows)

---

## Security

- `--target` validated at startup: only `http://` or `https://`. No SSRF via request headers.
- Strip `X-Forwarded-Host`, `X-Real-IP` before forwarding (probe is local dev, not a production proxy).
- Default bind `127.0.0.1` — never `0.0.0.0` by default.
- Field values never stored. Schema only (type + format + presence/absence).
- Body truncated at 1MB before any parsing (DoS protection on large uploads).
- No eval, no exec of inferred content.
- SQLite DB permissions: `0600` (owner read/write only).

---

## Framework extractor architecture

Each extractor in `scanner/<lang>/<framework>.go` implements:

```go
type Extractor interface {
    // Name returns the framework identifier (e.g. "nestjs", "spring", "aspnet-mvc")
    Name() string
    // Detect returns true if this extractor applies to the given project root.
    // Called during auto-detection; must be fast (check file existence only).
    Detect(root string) bool
    // Extract walks dir and returns all discovered endpoints.
    // Must be safe to call concurrently with other extractors.
    Extract(dir string, cfg *config.ScanConfig) ([]ScannedEndpoint, error)
}
```

**Prefix propagation** is each extractor's responsibility. Express `app.use('/api', router)`, NestJS `@Controller('/users')`, Spring `@RequestMapping('/api')`, Rails `namespace :api`, etc. — combine prefix + method path before storing. Never store `/users` when the real path is `/api/v1/users`.

**Path parameter normalization** from framework-specific syntax to probe's `{param}` format:
```
Express:   /users/:id        → /users/{id}
Rails:     /users/:id        → /users/{id}
Spring:    /users/{id}       → /users/{id}  (already correct)
ASP.NET:   /users/{id}       → /users/{id}  (already correct)
FastAPI:   /users/{user_id}  → /users/{user_id}
Sinatra:   /users/:id        → /users/{id}
Actix:     /users/{id}       → /users/{id}
```

## What NOT to do

- Don't add request modification or transformation — probe is read-only
- Don't add authentication simulation — that's `spec`'s job
- Don't add load testing — use k6/wrk
- Don't add real-time validation against a reference spec — that's a linter, not probe
- Don't store raw body content — schemas only
- Don't normalize paths during capture — store raw, compute patterns on read
- Don't bind to 0.0.0.0 by default
- Don't execute code during `probe scan` — text analysis only
- Don't require language runtimes (node, python, java, dotnet) to be installed for scanning
- Don't let one broken extractor abort the whole scan — isolate failures per file
- Don't let scan results overwrite traffic-observed schemas — observed always wins

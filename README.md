# probe

Transparent reverse proxy that discovers and documents API endpoints by observing real traffic.

No code changes to target services. No OpenAPI spec required. Pure observation.

---

## How it works

`probe intercept` sits between your client and server. Every request/response passes through unchanged. probe infers field types, tracks schema coverage across calls, and normalises path parameters — then lets you export everything as an OpenAPI spec.

```
client → probe intercept → your API server
                ↓
           probe list / show / export
```

---

## Install

**macOS / Linux**
```sh
curl -fsSL https://raw.githubusercontent.com/AgusRdz/probe/main/install.sh | sh
```

**Windows (PowerShell)**
```powershell
irm https://raw.githubusercontent.com/AgusRdz/probe/main/install.ps1 | iex
```

---

## Usage

```sh
# Start capturing traffic — each target gets its own DB automatically
probe intercept --target http://localhost:3000

# Self-signed cert / IIS Express / mkcert
probe intercept --target https://localhost:44300 --insecure

# List discovered endpoints
probe list

# Choose which columns to show
probe list --cols method,path,source,file,calls

# Inspect one endpoint
probe show GET /users/{id}

# Export — shorthand flags auto-name the file from your directory name
probe export --openapi    # → my-api.yaml
probe export --postman    # → my-api.postman_collection.json
probe export --curl       # → my-api.sh
probe export --httpie     # → my-api.httpie.sh
probe export --swagger    # → my-api.swagger.yaml
probe export --bruno      # → my-api-bruno/  (directory)
probe export --json       # → my-api.json

# Override the output path
probe export --postman --out ./exports/collection.json

# Set the collection/spec title
probe export --postman --title "Users API"

# Only export endpoints seen in real traffic (skip scan-only)
probe export --postman --min-calls 1

# Scan source code for routes (no traffic needed)
probe scan ./myapp

# Update to latest version
probe update
```

---

## Export formats

Seven formats supported. Use shorthand flags for the simplest experience — the output file is named automatically from your project directory.

| Shorthand | Format | Default filename | Description |
|---|---|---|---|
| `--openapi` | OpenAPI 3.x YAML | `<dir>.yaml` | Works with Swagger UI, Redoc, Stoplight |
| `--json` | OpenAPI 3.x JSON | `<dir>.json` | Same spec, JSON encoding |
| `--swagger` | Swagger 2.0 YAML | `<dir>.swagger.yaml` | For tools that only accept Swagger 2.0 |
| `--postman` | Postman Collection v2.1 | `<dir>.postman_collection.json` | Body, headers, query params |
| `--curl` | curl shell script | `<dir>.sh` | One `curl` command per endpoint |
| `--httpie` | HTTPie shell script | `<dir>.httpie.sh` | One `http` command per endpoint |
| `--bruno` | Bruno collection | `<dir>-bruno/` | Directory of `.bru` files |

```sh
# Shorthand — file named from your project directory automatically
probe export --postman          # → my-api.postman_collection.json
probe export --bruno            # → my-api-bruno/

# Override the path
probe export --postman --out ./exports/collection.json

# Set collection/spec title
probe export --postman --title "Users API"

# Only observed traffic (skip scan-only endpoints)
probe export --postman --min-calls 1
```

### What's included in Postman exports

probe builds Postman collections from observed traffic — the more traffic you send through `probe intercept`, the richer the output:

- **Request body** — JSON template with placeholder values inferred from observed schemas
- **Request headers** — headers seen in real traffic, with safe placeholder values:
  - `Authorization` → `Bearer {{token}}`
  - `X-Api-Key` → `{{api_key}}`
  - `Accept` → `application/json`
  - Custom `X-Tenant-ID` → `{{x_tenant_id}}`
  - Noisy/internal headers (`User-Agent`, `Accept-Encoding`, `Cookie`, browser `Sec-*`) are excluded
- **Query parameters** — param names extracted from observed URLs; values left blank for you to fill in
- **Path parameters** — `{id}` segments become Postman variables `{{id}}`

### Config-based output paths

Set `export.output_dir` to send all formats to one directory. Use `export.outputs` to override specific formats.

```yaml
# .probe.yml
export:
  output_dir: ./exports        # all formats land here, auto-named

  outputs:                     # per-format overrides (wins over output_dir)
    postman: ./postman/collection.json
```

With `output_dir` set, `probe export --postman` writes to `./exports/my-api.postman_collection.json` — no `--out` needed. You only need to configure the formats you actually use.

> **Windows paths:** Use relative paths (`./exports`) or forward slashes (`C:/Users/me/exports`) in YAML — unquoted backslashes are invalid YAML and will cause a parse error.
> ```yaml
> export:
>   output_dir: ./exports              # ✓ recommended — works everywhere
>   output_dir: C:/Users/me/exports    # ✓ absolute with forward slashes
>   output_dir: "C:\\Users\\me\\exports" # ✓ absolute with escaped backslashes
>   output_dir: C:\Users\me\exports    # ✗ will fail — unquoted backslashes
> ```

---

## Intercept setup — pointing your client at probe

probe is a transparent proxy: it listens on a local port, forwards requests to your real backend, and records the traffic. The only setup required is pointing your client at probe's port instead of the real backend.

```
client → probe (:4000) → your API (:3001)
```

Each target gets its own DB file automatically.

### Angular (`proxy.conf.json`)
```json
{
  "/api": {
    "target": "http://localhost:4000",
    "changeOrigin": true
  }
}
```
*(was `"target": "http://localhost:3001"`)*

### Vite (`vite.config.ts`)
```ts
server: {
  proxy: {
    '/api': 'http://localhost:4000'
  }
}
```

### Create React App (`package.json`)
```json
"proxy": "http://localhost:4000"
```

### Environment variable (any framework)
```sh
API_URL=http://localhost:4000 npm start
VITE_API_URL=http://localhost:4000 npm run dev
```

### IIS Express / IIS (dev certificate)
```sh
probe intercept --target https://localhost:44300 --insecure
# --insecure skips TLS cert verification for self-signed / dev certs
```

### Docker Compose
```sh
# probe on the host, target the mapped port
probe intercept --target http://localhost:8080
```

### Traefik / nginx (local)
```sh
probe intercept --target http://localhost:80 --filter /api
```

### Remote dev / QA environment
```sh
probe intercept --target https://api.dev.company.com
# then set API_URL=http://localhost:4000 in your frontend
```

### Multiple services
```sh
probe intercept --target http://localhost:3001 --port 4001
probe intercept --target http://localhost:3002 --port 4002
# each gets its own DB; use probe list --db <path> to query separately
```

> **Full recipe reference:** `probe help intercept`

---

## Commands

| Command | Description |
|---|---|
| `intercept --target <url>` | Proxy traffic and capture endpoint schemas |
| `list` | List all discovered endpoints |
| `show <METHOD> <PATH>` | Full detail: schema + coverage breakdown |
| `export` | Export as OpenAPI 3.x YAML |
| `scan <dir>` | Static analysis — extract routes from source code |
| `annotate "METHOD /path"` | Add description, tags, or path override |
| `stats` | Show endpoint count summary |
| `clear` | Delete all observations |
| `init` | Create `.probe.yml` with all settings as commented examples |
| `config show` | Show config paths and active editor |
| `config edit` | Open project config in editor |
| `update` | Download and install the latest release |
| `version` | Show version |
| `help [command]` | Show help for a command |

---

## probe list — columns

`probe list` shows a configurable set of columns. Default: `method, path, source, file, calls, coverage`.

| Column | Description |
|---|---|
| `method` | HTTP verb (GET, POST, PUT, …) |
| `path` | URL pattern with `{param}` placeholders |
| `source` | Where probe learned about the endpoint: `scan` / `observed` / `scan+obs` |
| `file` | Source file and line number (scan only, e.g. `UsersController.cs:42`) |
| `calls` | Number of observed traffic calls |
| `coverage` | Schema evidence bar — how well-documented this endpoint is. Green bar = strong evidence. Formula: `min(calls/30, 1) × source_quality` where scan=35%, scan+obs=60%, observed=100%. Grows as you send more traffic through `probe intercept`. |
| `protocol` | `rest` / `graphql` / `grpc` |
| `status` | Observed HTTP status codes |
| `framework` | Detected framework (e.g. `aspnet-mvc`, `nestjs`, `spring`) |

**Select columns:**
```sh
probe list --cols method,path,file,calls
```

**Persist via config:**
```yaml
# .probe.yml
list:
  columns: method,path,source,file,calls
```

---

## Configuration

Two-level config — project overrides global:

**Global:** `~/.config/probe/config.yml`
**Project:** `.probe.yml` (walked up from cwd)

### Creating and editing config

```sh
# Create project config (.probe.yml in cwd)
probe init

# Create global config (~/.config/probe/config.yml)
probe init --global

# Open project config in editor (creates it if missing)
probe config edit

# Open global config in editor
probe config edit --global

# Show config paths and active editor
probe config show
```

All settings are commented out by default — uncomment and edit what you need.

### Refreshing config after an upgrade

`probe init` is non-destructive. If the config file already exists, it writes a `.new` file alongside it so you can diff and copy any new settings manually.

```sh
probe init
# → Config already exists: .probe.yml
# → Wrote current template: .probe.yml.new

# Diff on Unix / Git Bash
diff .probe.yml .probe.yml.new

# Diff in VS Code (works on all platforms)
code --diff .probe.yml .probe.yml.new

# Diff in PowerShell
Compare-Object (Get-Content .probe.yml) (Get-Content .probe.yml.new)
```

Copy any new settings you want into your config, then delete `.probe.yml.new`.

### Config reference

```yaml
# .probe.yml
proxy:
  port: 4000
  target: http://localhost:3001
  filter: /api
  ignore:
    - /health
    - /metrics

inference:
  path_normalization_threshold: 3
  confidence_threshold: 0.9

list:
  columns: method,path,source,file,calls,coverage
```

---

## Supported frameworks (scan)

| Language | Frameworks |
|---|---|
| JavaScript / TypeScript | Express, NestJS, Next.js, Fastify, Koa, Hapi |
| Python | FastAPI, Flask, Django |
| Go | Gin, Echo, Fiber, Chi, stdlib |
| Ruby | Rails, Sinatra |
| PHP | Laravel, Symfony, CodeIgniter, Zend / Laminas |
| Java | Spring MVC |
| Kotlin | Ktor |
| Rust | Actix-web, Axum |
| C# | ASP.NET Core MVC, ASP.NET Minimal API, ASP.NET Web API (.NET Framework 4.x) |

Supports both .NET Core (`[Route]`, `IActionResult`) and .NET Framework (`[RoutePrefix]`, `IHttpActionResult`) attribute routing styles, including conventional routing via `MapHttpRoute` and `[ActionName]`.

---

## Pairs with

[spec](https://github.com/AgusRdz/spec) — mock server that serves a spec produced by probe.

```sh
probe intercept --target http://localhost:3000  # observe
probe export --out api.yaml                     # export
spec serve api.yaml                             # mock
```

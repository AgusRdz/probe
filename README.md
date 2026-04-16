# probe

Transparent reverse proxy that discovers and documents API endpoints by observing real traffic.

No code changes to target services. No OpenAPI spec required. Pure observation.

---

## How it works

`probe intercept` sits between your client and server. Every request/response passes through unchanged. probe infers field types, tracks confidence across calls, and normalises path parameters — then lets you export everything as an OpenAPI spec.

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

# List discovered endpoints
probe list

# Inspect one endpoint
probe show GET /users/{id}

# Export as OpenAPI
probe export --out openapi.yaml

# Scan source code for routes (no traffic needed)
probe scan ./myapp

# Update to latest version
probe update
```

---

## Commands

| Command | Description |
|---|---|
| `intercept --target <url>` | Proxy traffic and capture endpoint schemas |
| `list` | List all discovered endpoints |
| `show <METHOD> <PATH>` | Full detail: schema + confidence breakdown |
| `export` | Export as OpenAPI 3.x YAML |
| `scan <dir>` | Static analysis — extract routes from source code |
| `annotate "METHOD /path"` | Add description, tags, or path override |
| `stats` | Show endpoint count summary |
| `clear` | Delete all observations |
| `update` | Download and install the latest release |
| `version` | Show version |

---

## Configuration

Two-level config — global overrides are project-local:

**Global:** `~/.config/probe/config.yml`
**Project:** `.probe.yml` (walked up from cwd)

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
```

---

## Supported frameworks (scan)

| Language | Frameworks |
|---|---|
| JavaScript / TypeScript | Express, NestJS, Next.js |
| Python | FastAPI, Flask, Django |
| Go | Gin, Echo, Fiber, Chi, stdlib |
| Ruby | Rails |
| PHP | Laravel |
| Java | Spring MVC |
| Kotlin | Ktor |
| Rust | Actix-web, Axum |
| C# | ASP.NET Core MVC, ASP.NET Minimal API, ASP.NET Web API (.NET Framework 4.x) |

Supports both .NET Core (`[Route]`, `IActionResult`) and .NET Framework (`[RoutePrefix]`, `IHttpActionResult`) attribute routing styles, including flexible decorator ordering.

---

## Pairs with

[spec](https://github.com/AgusRdz/spec) — mock server that serves a spec produced by probe.

```sh
probe intercept --target http://localhost:3000  # observe
probe export --out api.yaml                     # export
spec serve api.yaml                             # mock
```

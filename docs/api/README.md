# CyberMesh API Documentation

OpenAPI-powered API docs rendered with [Scalar](https://scalar.com).

## Quick Start

```bash
# Option 1: Use any static file server
npx serve docs/api

# Option 2: Python
python -m http.server 3100 -d docs/api

# Option 3: Just open the file
start docs/api/index.html
```

Then open `http://localhost:3100` and use the dropdown to switch between services.

## Adding a New Service

1. **Create a spec** — Add `docs/api/specs/<service-name>.yaml` using OpenAPI 3.1
2. **Register it** — Add an `<option>` to the dropdown in `index.html`:

```html
<option value="specs/my-new-service.yaml">My Service — N endpoints</option>
```

That's it. No build step, no installs.

## Service Specs

| File | Service | Endpoints | Stack |
|------|---------|-----------|-------|
| `backend.yaml` | Backend API | 30 | Go `net/http` |
| `ai-service.yaml` | AI Service | 10 | Python `http.server` |
| `sentinel.yaml` | Sentinel API | 12 | Python FastAPI |
| `enforcement.yaml` | Enforcement Agent | 7 | Go `net/http` |
| `telemetry.yaml` | Telemetry Layer | 1 | Go (Prometheus) |

## Spec Conventions

- **OpenAPI version:** 3.1.0
- **Filename:** lowercase, hyphenated service name
- **Tags:** group endpoints logically
- **Security:** document auth scheme per service
- **Servers:** include both local and in-cluster URLs

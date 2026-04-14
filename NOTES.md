# Auth Layer: AWS Cognito Integration Plan

## Context

The application is a Go/chi web app with sessions (SCS + MySQL), CSRF protection, and a stub `requireAuth` middleware. The user wants to add real authentication and authorization via AWS Cognito.

---

## Code Review Findings

### What's already there (good)
- `requireAuth` middleware in `lib/service/middleware.go:11` — structure is right, just needs real logic
- SCS session manager (MySQL store, HttpOnly, SameSite=Strict, Secure in prod) — solid
- noSurf CSRF middleware — correctly configured
- `templates/pages/login.go.html` exists — a starting point for the login page

### Issues / things to address
1. **`requireAuth` redirects to `/ai-login`** (`middleware.go:20`) — that route doesn't exist. Smells like leftover from a previous project. Rename to `/login`.
2. **Session key `currentAILogID`** (`middleware.go:16`) — also looks like a leftover; should be cleaned up to just check `isAuthenticated` (or a `userSub` claim from Cognito).
3. **CORS `AllowedOrigins: []string{"*"}`** (`service.go:46`) with `AllowCredentials: false` — fine for public API endpoints, but worth tightening to specific origins once the domain is known. CORS doesn't apply to same-origin requests anyway, so this doesn't block browser session cookies.
4. **SMTP `InsecureSkipVerify: true`** in `smtp.go` — unrelated to auth, but a security concern worth fixing separately.
5. **No `/login`, `/logout`, or callback routes** — need to add all three.

### What Cognito brings (and what it doesn't)
Cognito handles: user storage, password hashing/reset, email verification, MFA, social federation. What you still own: your session lifecycle, your authorization logic (who can do what), and JWT validation code.

---

## Recommended Approach: OAuth 2.0 Authorization Code Flow (Hosted UI)

Use Cognito's Hosted UI rather than direct API calls. This avoids managing credentials in your own UI and delegates the hard stuff (MFA, password reset, account recovery) to Cognito.

### Flow
```
User visits /protected
  → requireAuth sees no session
  → redirect to /login
  → /login redirects to Cognito Hosted UI with state param
  → user authenticates on Cognito
  → Cognito redirects to /auth/callback?code=...&state=...
  → app validates state, exchanges code for tokens
  → app validates ID token JWT (JWKS)
  → app writes userSub + email + groups to session
  → redirect to original URL
```

### Authorization via Cognito Groups
Cognito Groups are included in the ID token as `cognito:groups`. The app can read these after validation and store a `userGroups []string` in the session. A `requireGroup("admin")` middleware wrapper can then enforce group membership.

---

## Session Keys (convention)
```
"isAuthenticated"    bool
"userSub"            string   // Cognito sub (UUID, stable user identifier)
"userEmail"          string
"userGroups"         []string // from cognito:groups claim
"redirectAfterLogin" string
```

---

## Config (`~/.gomario.json`)
Add a `cognito` block:
```json
"cognito": {
  "region": "us-east-1",
  "userPoolId": "us-east-1_XYZ",
  "clientId": "...",
  "clientSecret": "...",
  "domain": "auth.yourdomain.com",
  "callbackUrl": "https://yourdomain.com/auth/callback",
  "logoutUrl": "https://yourdomain.com"
}
```

---

## What Was Implemented — Cognito Auth

### `lib/config/config.go`
Added `Cognito` struct to the config.

### `lib/service/jwks.go` *(new)*
Fetches and caches Cognito's JWKS on startup, then validates ID token JWTs (signature, expiry, issuer, audience) against the live keyset using `github.com/lestrrat-go/jwx/v3`.

### `lib/service/auth.go` *(new)*
- `GET /login` — generates a random state, stores it in session, redirects to Cognito Hosted UI via `oauth2.Config.AuthCodeURL`
- `GET /auth/callback` — validates state, exchanges the code via `oauth2.Config.Exchange`, validates the ID token, writes `isAuthenticated` / `userSub` / `userEmail` / `userGroups` to session, then redirects to the original URL
- `POST /logout` — destroys the local session and redirects to Cognito's logout endpoint to clear the SSO session; POST prevents logout CSRF

### `lib/service/middleware.go`
- `requireAuth` now redirects to `/login` and checks only `isAuthenticated`
- Removed stale `currentAILogID` session key check
- Added `requireGroup(group string)` for route-level authorization based on Cognito group membership

### `lib/service/service.go`
- `JWKSCache *jwksCache` field added to `Service` struct
- JWKS cache initialized on startup via `newJWKSCache(context.Background(), cfg)`
- Three auth routes registered: `/login`, `/auth/callback`, `/logout`

### Verification
1. `go build ./...` — confirm no compile errors
2. Start app, visit a protected route → confirm redirect to Cognito Hosted UI
3. Complete login → confirm redirect back to original URL with session set
4. Visit `/logout` → confirm session cleared and Cognito session terminated
5. Confirm `userSub`, `userEmail`, `userGroups` are populated in session
6. Add a test route protected by `requireGroup("admin")` and verify group-based access control

---

# JSON API Layer

## Context

The app serves both HTML pages and JSON endpoints consumed by in-page JavaScript. Both participate in the same session-based auth, but their failure modes differ: HTML routes redirect unauthenticated users to `/login`, while API routes return JSON error responses (401/403) so JavaScript can handle them gracefully.

---

## Design

### Path prefix
All JSON endpoints live under `/api/`. Makes auth middleware easy to scope and the API surface easy to proxy or CORS-configure independently in future.

### Same auth mechanism, different failure responses
The browser sends the session cookie automatically on same-origin fetch calls — no token exchange needed.

| Route type     | Unauthenticated  | Unauthorized (wrong group) |
|----------------|------------------|----------------------------|
| HTML (`/foo`)  | 303 → `/login`   | 403 HTML page              |
| API (`/api/foo`) | 401 JSON       | 403 JSON                   |

### CSRF for JavaScript fetch calls
noSurf validates the `X-CSRF-Token` request header (already in CORS `AllowedHeaders`). Since the CSRF cookie is `HttpOnly: true`, JS cannot read it directly. Instead, fetch the token from the dedicated endpoint and cache it:

```js
const { csrfToken } = await fetch('/api/csrf-token').then(r => r.json());

// Include on every state-mutating request:
fetch('/api/some-data', {
  method: 'POST',
  headers: { 'X-CSRF-Token': csrfToken }
});
```

---

## What Was Implemented — JSON API Layer

### `lib/service/apihandler.go` *(new)*
`apiHandler` type, parallel to `serviceHandler`. Unhandled errors write a JSON 500 with `Content-Type: application/json` instead of plain text.

### `lib/service/middleware.go`
Two new chi-compatible (`func(http.Handler) http.Handler`) middlewares:
- `apiAuthMiddleware` — returns JSON 401 on missing session (no redirect)
- `apiRequireGroup(group)` — wraps `apiAuthMiddleware`, returns JSON 403 if the user lacks the required Cognito group

### `lib/service/service.go`
- `renderJSON(w, data, status)` added alongside `renderJSONError`
- `/api/` sub-router wired up in `setRoutes` with `apiAuthMiddleware` applied to the whole group

### `lib/service/auth.go`
- `GET /api/csrf-token` — returns `{"csrfToken": "..."}` for use by JavaScript

### Verification
1. `go build ./...` — no compile errors
2. Unauthenticated fetch to `/api/any` → 401 JSON, no redirect
3. Authenticated fetch to `/api/any` with wrong group → 403 JSON
4. `GET /api/csrf-token` → returns token; include as header on POST → noSurf accepts it
5. HTML routes continue to redirect to `/login` as before

---

## Usage pattern for new API endpoints

```go
// Authenticated, any user
r.Method(http.MethodGet, "/some-data", apiHandler(s.someData))

// Authenticated, restricted to a Cognito group
r.With(s.apiRequireGroup("admin")).Method(http.MethodGet, "/admin-data", apiHandler(s.adminData))
```

---

# JavaScript Build — esbuild + Makefile

## Design

All client-side JavaScript source lives in `assets/js/inc/` as ES6 modules. esbuild bundles everything into a single `assets/js/bundle.js`, which is the only file referenced in the page template. `bundle.js` is gitignored — it is always a build artefact.

```
assets/js/
  inc/          ← source files (ES6 modules, tracked in git)
    main.js     ← entry point, imported by esbuild
    api.js      ← fetch helpers for /api/ endpoints
    utils.js    ← shared utilities (flash messages, cookie read)
  bundle.js     ← esbuild output (gitignored)
```

npm packages from `package.json` (e.g. `@tabler/core`) are imported in `main.js` and bundled automatically by esbuild from `node_modules/`.

## What Was Implemented

### `Makefile` *(new)*

| Target | Action |
|--------|--------|
| `make build` | Minified JS bundle + Go binary |
| `make js` | JS bundle only |
| `make go` | Go binary only |
| `make dev` | esbuild `--watch` in background + `go run .` |
| `make clean` | Remove `bundle.js`, `bundle.js.map`, Go binary |

### `assets/js/inc/main.js` *(new)*
Entry point. Imports `@tabler/core` and the local modules. Exposes `App.flash`, `App.apiGet`, `App.apiPost` on `window` for use in inline template scripts.

### `assets/js/inc/api.js` *(new)*
Thin fetch wrappers for the `/api/` surface. `apiPost` automatically fetches and caches the CSRF token from `GET /api/csrf-token` on first use.

### `assets/js/inc/utils.js` *(new)*
`flash(message, type)` — inserts a dismissible Tabler alert at the top of the page.
`getCookie(name)` — reads a cookie value by name.

### `package.json`
- `esbuild ^0.25.0` added to `devDependencies`
- `npm run build` — production bundle (minified)
- `npm run dev` — development bundle with source maps + watch

### `templates/common/js-includes.go.html`
Now references only `bundle.js`:
```html
<script src="/assets/js/bundle.js"></script>
```

### `.gitignore`
`assets/js/bundle.js` and `assets/js/bundle.js.map` added.

## Setup (first time)

```sh
npm install   # installs esbuild and other devDependencies
make build    # produces bundle.js and ./tmp/gomario
```

---

# Telemetry — OpenTelemetry

## Design

Vendor-neutral OpenTelemetry integration covering traces, metrics, and health probes. All signals export via OTLP HTTP to any compatible collector (AWS ADOT, Grafana Agent, local `otel-collector`). The backend can be swapped without touching app code.

| Signal | Mechanism |
|--------|-----------|
| **Traces** | `otelhttp.NewHandler` wraps the chi mux — every request gets a span. DB queries get child spans via `otelsql`. |
| **Metrics** | HTTP metrics (count, duration, errors) via `otelhttp`. DB pool stats (open, in-use, idle) as observable gauges. |
| **Health** | `GET /healthz` (liveness), `GET /readyz` (readiness + DB ping). No auth required. |

In development (`IsProduction: false`) spans are also written to stdout so traces are visible without a running collector.

## Config (`~/.gomario.json`)

```json
"telemetry": {
  "enabled": true,
  "serviceName": "gomario",
  "endpoint": "http://localhost:4318"
}
```

## What Was Implemented

### `lib/telemetry/telemetry.go` + `resource.go` *(new)*
`Setup(ctx, cfg)` initialises the OTel trace and metric providers and sets them as globals. Returns a `shutdown` function that flushes both providers — called during graceful shutdown in `main.go`.

### `lib/model/model.go`
MySQL driver opened via `otelsql.Open` (github.com/XSAM/otelsql) instead of directly. Every SQL query now produces a child span with query text and duration.

### `lib/service/health.go` *(new)*
- `GET /healthz` — always 200 `{"status":"ok"}` (liveness)
- `GET /readyz` — pings DB; 200 or 503 `{"status":"unavailable"}` (readiness)

### `lib/service/service.go`
- `Service.Handler http.Handler` field added — the otelhttp-wrapped mux. **Use `svc.Handler` (not `svc.Muxer`) as the `http.Server` handler.**
- `registerDBPoolMetrics()` registers observable gauges for `db.pool.open_connections`, `db.pool.in_use`, `db.pool.idle`.
- `/healthz` and `/readyz` routes registered (no auth middleware).

### `main.go`
- `telemetry.Setup(ctx, cfg)` called before `NewService`.
- Telemetry shutdown wired into the graceful shutdown goroutine (after HTTP server closes, before process exits).
- `httpServer.Handler` now uses `svc.Handler` instead of `svc.Muxer`.

## Verification
1. `go build ./...` — no compile errors
2. `GET /healthz` → `{"status":"ok"}` with no auth
3. `GET /readyz` → `{"status":"ok"}` when DB is up, `503` when down
4. Dev mode: spans printed to stdout for each request, with DB child spans visible
5. With a local collector (`otelcol --config=...`): traces appear at the OTLP endpoint

---

# Error Handling

## Design

Structured error handling with typed errors, custom error pages, and OTel integration. Should have been implemented first — every feature now built on top benefits automatically.

### `HTTPError` — typed errors
Handlers return an `HTTPError` to control the response status and user-facing message. Anything else is treated as an unexpected 500.

```go
return service.ErrNotFound("that page doesn't exist")   // → 404 page
return service.ErrForbidden("access denied")             // → 403 page
return fmt.Errorf("db query failed: %w", err)            // → 500 page (logged)
```

### Handler wrappers
`s.handle(fn)` for HTML routes, `s.handleAPI(fn)` for JSON API routes. Both close over `*Service` so the error path can log, record OTel spans, and render templates.

### Error page selection
- `HTTPError` with status < 500 → `4xx.go.html`
- `HTTPError` with status ≥ 500, or any unexpected error → `5xx.go.html`
- If the error template itself fails → plain text fallback

## What Was Implemented

### `lib/service/errors.go` *(new)*
`HTTPError` struct + constructors: `ErrBadRequest`, `ErrUnauthorized`, `ErrForbidden`, `ErrNotFound`, `ErrMethodNotAllowed`.

### `lib/service/handler.go`
- Removed `serviceHandler` type
- `s.handle(fn)` — wraps HTML route handlers; errors call `s.handleHTTPError`
- `s.handleHTTPError(w, r, err)` — records on OTel span, logs 5xx, renders error template
- `s.renderErrorPage(w, status, message)` — picks `4xx.go.html` or `5xx.go.html`

### `lib/service/apihandler.go`
- Removed `apiHandler` type
- `s.handleAPI(fn)` — wraps JSON API handlers; errors call `s.handleAPIError`
- `s.handleAPIError(w, r, err)` — records on OTel span, logs 5xx, writes JSON error body

### `lib/service/middleware.go`
- Removed `Middleware` type (wrapped the old `serviceHandler`)
- `requireAuth` converted to `func(http.Handler) http.Handler` — uniform with API middleware
- `requireGroup` converted to `func(string) func(http.Handler) http.Handler`
- `apiAuthMiddleware` and `apiRequireGroup` now use `s.handleAPIError` for consistent JSON error responses

### `lib/service/service.go`
- `mux.NotFound` → renders `4xx.go.html` with 404
- `mux.MethodNotAllowed` → renders `4xx.go.html` with 405
- All route registrations updated: `serviceHandler(s.x)` → `s.handle(s.x)`, `apiHandler(s.x)` → `s.handleAPI(s.x)`

### `lib/service/render.go`
Fixed bug: `Content-Type: text/html; charset=utf-8` now set *before* `WriteHeader` so it is actually sent to the client.

## Usage

```go
// Signal a specific HTTP status from any handler:
return service.ErrNotFound("the item you requested does not exist")

// Protect an HTML route group:
mux.Group(func(r chi.Router) {
    r.Use(s.requireAuth)
    r.Method(http.MethodGet, "/dashboard", s.handle(s.dashboard))
})

// Protect an HTML route with group membership:
mux.With(s.requireGroup("admin")).Method(http.MethodGet, "/admin", s.handle(s.admin))
```

---

# Auth Fixes and Improvements

## Issues Found and Fixed

### `SameSite=Strict` breaks the OAuth callback
The session cookie was set with `SameSite=Strict`. When Cognito redirects back to `/auth/callback` after login, the browser treats it as a cross-site top-level redirect and does not send the session cookie. The callback handler receives a fresh empty session, `oauthState` is missing, and the state check fails with "invalid state parameter" even though the values match when logged.

**Fix (`lib/service/service.go`):** Changed `http.SameSiteStrictMode` → `http.SameSiteLaxMode`. `Lax` still blocks cross-site POST requests and embedded resource loads, but allows the cookie to be sent on top-level GET redirects — which is exactly what the OAuth callback is.

```go
// Lax (not Strict) is required: the OAuth callback from Cognito is a
// cross-site top-level redirect, which SameSite=Strict would block.
sessionMgr.Cookie.SameSite = http.SameSiteLaxMode
```

### `logout_uri` misconfiguration causes a redirect loop
`logout_uri` is the URL Cognito redirects the user *to* after clearing the SSO session — it is a destination, not the logout initiator. If set to the app's `/logout` path, Cognito redirects back to `/logout` after logout, which triggers another Cognito logout, causing an infinite loop.

**Fix:** Set `logoutUrl` in `~/.gomario.json` to a landing page (e.g. `https://yourdomain.com/`), and register the same URL in Cognito's **Allowed sign-out URLs**. The two values must match exactly (scheme, host, path, trailing slash).

### `GET /logout` is vulnerable to logout CSRF
noSurf only protects unsafe HTTP methods (POST, PUT, DELETE). A `GET /logout` endpoint can be triggered by a malicious `<img src="/logout">` on any page, silently logging the user out.

**Fix (`lib/service/service.go`):** Changed `/logout` route from `GET` to `POST`. The logout button in the nav is a small HTML form that includes the CSRF token; noSurf validates it automatically.

### Manual token exchange replaced with `golang.org/x/oauth2`
The original `exchangeCode` function manually built the HTTP POST, set the `Authorization` header, and decoded the JSON response. Replaced with `oauth2.Config.Exchange` from the standard `golang.org/x/oauth2` package, which handles all of this correctly and consistently.

**Fix (`lib/service/auth.go`):** Added `cognitoOAuth2Config() *oauth2.Config` method. `login` uses `AuthCodeURL(state)` to build the redirect URL; `authCallback` uses `Exchange(ctx, code)` to obtain tokens. `id_token` is extracted via `tokens.Extra("id_token").(string)`.

---

## What Was Implemented — Template Data and Nav Auth State

### `lib/service/render.go`
Added `templateData` base struct and `newTemplateData(r)` helper:

```go
type templateData struct {
    Title           string
    IsAuthenticated bool
    CSRFToken       string
    UserEmail       string
}
```

`newTemplateData(r)` reads `isAuthenticated` and `userEmail` from the session and the CSRF token from noSurf. Every page handler calls this instead of building ad-hoc data structs.

### `lib/service/index.go`, `lib/service/start.go`
Updated to use `s.newTemplateData(r)` and set `Title` on the returned struct. Removed the inline anonymous structs and the direct `nosurf` import from `start.go`.

### `templates/common/top-menu.go.html`
Nav now conditionally renders based on `IsAuthenticated`:
- **Authenticated:** shows `UserEmail` and a `POST /logout` form with CSRF token
- **Unauthenticated:** shows a "Sign in" link to `/login`
- Fixed stale `/ai-start` link to `/start`

### Usage pattern for new page handlers

```go
func (s *Service) myPage(w http.ResponseWriter, r *http.Request) error {
    data := s.newTemplateData(r)
    data.Title = "My Page"
    // add page-specific fields by extending the struct
    return s.render(w, "mypage.go.html", data, nil, http.StatusOK)
}
```

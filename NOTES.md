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
- `GET /login` — generates a random state, stores it in session, redirects to Cognito Hosted UI
- `GET /auth/callback` — validates state, exchanges the code for tokens, validates the ID token, writes `isAuthenticated` / `userSub` / `userEmail` / `userGroups` to session, then redirects to the original URL
- `GET /logout` — destroys the local session and redirects to Cognito's logout endpoint to clear the SSO session too

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

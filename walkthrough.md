# Meal Tracker — Code Walkthrough

*2026-02-25T02:33:02Z by Showboat 0.6.1*
<!-- showboat-id: 964b8488-ffb3-4150-9f12-d610076772fd -->

## Project Overview

This is a full-stack meal tracking web application. The backend is written in Go with SQLite for persistence. The frontend is a SolidJS single-page application built with Vite. The two halves communicate over a JSON REST API, with cookie-based session authentication sitting in between.

The file tree is small — four Go files and seven frontend source files:

```bash
find server/ frontend/src/ frontend/index.html frontend/vite.config.ts -type f | grep -v node_modules | sort
```

```output
frontend/index.html
frontend/src/App.tsx
frontend/src/Auth.tsx
frontend/src/Tracker.tsx
frontend/src/api.ts
frontend/src/index.tsx
frontend/src/styles.css
frontend/vite.config.ts
server/api.go
server/db.go
server/go.mod
server/go.sum
server/main.go
server/ods.go
server/server
```

We'll walk through the code in the order a request travels: server entry point, database schema, API routing and handlers, then the frontend from its entry point through authentication and into the main tracker UI.

---

## 1. Server Entry Point — `server/main.go`

The server boots in three steps: open the database, wire up the API, start listening. Both the database path and listen address are configurable via environment variables, falling back to sensible defaults.

```bash
cat -n server/main.go
```

```output
     1	package main
     2	
     3	import (
     4		"log"
     5		"net/http"
     6		"os"
     7	)
     8	
     9	func main() {
    10		dbPath := "mealtracker.db"
    11		if p := os.Getenv("DB_PATH"); p != "" {
    12			dbPath = p
    13		}
    14	
    15		db, err := NewDB(dbPath)
    16		if err != nil {
    17			log.Fatalf("failed to open database: %v", err)
    18		}
    19		defer db.Close()
    20	
    21		api := NewAPI(db)
    22	
    23		addr := ":8080"
    24		if a := os.Getenv("ADDR"); a != "" {
    25			addr = a
    26		}
    27	
    28		log.Printf("server listening on %s", addr)
    29		if err := http.ListenAndServe(addr, api.Handler()); err != nil {
    30			log.Fatal(err)
    31		}
    32	}
```

Key details:

- **Line 10–13**: Database path defaults to `mealtracker.db` in the working directory. `NewDB` (defined in `db.go`) opens the SQLite connection and runs migrations automatically.
- **Line 15–19**: The database connection is opened once and deferred-closed. The `DB` struct wraps `*sql.DB` and provides all the query methods the API handlers need.
- **Line 21**: `NewAPI` receives the database handle and returns an `API` struct. The `Handler()` method on that struct returns a fully-wired `http.Handler`.
- **Line 29**: The Go standard library's `http.ListenAndServe` does the rest — no external HTTP framework needed.

---

## 2. Database Layer — `server/db.go`

This file defines the data model, the schema migration, and every database operation. Let's start with the three domain types:

```bash
sed -n "12,36p" server/db.go
```

```output
type DB struct {
	conn *sql.DB
}

type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	CreatedAt string `json:"created_at"`
}

type Meal struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	Date      string `json:"date"`
	MealName  string `json:"meal_name"`
	Content   string `json:"content"`
	SortOrder int    `json:"sort_order"`
}

type MealType struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"user_id"`
	Name      string `json:"name"`
	SortOrder int    `json:"sort_order"`
}
```

Three domain types carry all data through the system:

- **User** — just an ID and username (the password hash is never serialized to JSON).
- **Meal** — a text entry for one meal on one date. The `meal_name` field (e.g. "Breakfast") ties it to a meal slot; `content` holds the free-form text (up to 4095 chars). `sort_order` preserves display order.
- **MealType** — a user's configured meal slots. New users get four defaults; they can add or remove these.

All three structs carry `json` tags so Go's `encoding/json` can marshal them directly into API responses.

### Database Connection and Migration

The connection string enables three SQLite pragmas inline:

```bash
sed -n "38,50p" server/db.go
```

```output
func NewDB(path string) (*DB, error) {
	conn, err := sql.Open("sqlite3", path+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return db, nil
}
```

The connection URI configures SQLite for a concurrent web server:

- **`_journal_mode=WAL`** — Write-Ahead Logging allows readers and a writer to operate simultaneously, critical for handling concurrent HTTP requests without "database is locked" errors.
- **`_busy_timeout=5000`** — if a write lock is held, wait up to 5 seconds before returning SQLITE_BUSY instead of failing immediately.
- **`_foreign_keys=on`** — enforces the `REFERENCES` constraints in the schema so cascading deletes actually work.

After opening, `migrate()` runs `CREATE TABLE IF NOT EXISTS` for all four tables:

```bash
sed -n "56,96p" server/db.go
```

```output
func (db *DB) migrate() error {
	_, err := db.conn.Exec(`
		CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT (datetime('now'))
		);

		CREATE TABLE IF NOT EXISTS meal_types (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			sort_order INTEGER NOT NULL DEFAULT 0,
			UNIQUE(user_id, name)
		);

		CREATE TABLE IF NOT EXISTS meals (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			date TEXT NOT NULL,
			meal_name TEXT NOT NULL,
			content TEXT NOT NULL DEFAULT '',
			sort_order INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL DEFAULT (datetime('now')),
			updated_at TEXT NOT NULL DEFAULT (datetime('now')),
			UNIQUE(user_id, date, meal_name)
		);

		CREATE INDEX IF NOT EXISTS idx_meals_user_date ON meals(user_id, date);
		CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
	`)
	return err
}
```

The schema is four tables:

| Table | Purpose | Key Constraints |
|-------|---------|-----------------|
| `users` | Account credentials | `username UNIQUE` |
| `sessions` | Login sessions (UUID tokens) | FK → `users`, `ON DELETE CASCADE` |
| `meal_types` | Per-user meal slot config | `UNIQUE(user_id, name)` |
| `meals` | Actual meal entries | `UNIQUE(user_id, date, meal_name)`, indexed on `(user_id, date)` |

The `ON DELETE CASCADE` on every foreign key means deleting a user automatically cleans up all their sessions, meal types, and meal entries.

The `UNIQUE(user_id, date, meal_name)` constraint on `meals` prevents duplicate entries — you can't have two "Breakfast" entries for the same user on the same day.

The composite index `idx_meals_user_date` makes the most common query — "get all meals for user X on date Y" — fast.

### User Registration and Authentication

When a user registers, the password is hashed with bcrypt at the default cost (10 rounds), and four default meal types are created:

```bash
sed -n "100,129p" server/db.go
```

```output
func (db *DB) CreateUser(username, password string) (*User, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}

	result, err := db.conn.Exec(
		"INSERT INTO users (username, password_hash) VALUES (?, ?)",
		username, string(hash),
	)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()

	// Create default meal types for new user
	defaults := []string{"Breakfast", "Lunch", "Dinner", "Snack"}
	for i, name := range defaults {
		_, err := db.conn.Exec(
			"INSERT INTO meal_types (user_id, name, sort_order) VALUES (?, ?, ?)",
			id, name, i,
		)
		if err != nil {
			return nil, err
		}
	}

	return &User{ID: id, Username: username}, nil
}
```

Line 101 is where security happens: `bcrypt.GenerateFromPassword` produces a salted, slow-to-compute hash. The plaintext password is never stored. On login, `AuthenticateUser` retrieves the hash and compares:

```bash
sed -n "131,147p" server/db.go
```

```output
func (db *DB) AuthenticateUser(username, password string) (*User, error) {
	var user User
	var hash string
	err := db.conn.QueryRow(
		"SELECT id, username, password_hash, created_at FROM users WHERE username = ?",
		username,
	).Scan(&user.ID, &user.Username, &hash, &user.CreatedAt)
	if err != nil {
		return nil, err
	}

	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)); err != nil {
		return nil, fmt.Errorf("invalid password")
	}

	return &user, nil
}
```

Note the use of parameterized queries (`?` placeholders) throughout — this prevents SQL injection. The raw password is never logged, stored, or returned; only the bcrypt hash touches the database.

### Session Management

Sessions use UUIDs as tokens, stored in the database with an expiration timestamp:

```bash
sed -n "151,187p" server/db.go
```

```output
func (db *DB) CreateSession(sessionID string, userID int64) error {
	expires := time.Now().Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(
		"INSERT INTO sessions (id, user_id, expires_at) VALUES (?, ?, ?)",
		sessionID, userID, expires,
	)
	return err
}

func (db *DB) GetSession(sessionID string) (*User, error) {
	var user User
	var expires string
	err := db.conn.QueryRow(`
		SELECT u.id, u.username, u.created_at, s.expires_at
		FROM sessions s JOIN users u ON s.user_id = u.id
		WHERE s.id = ?
	`, sessionID).Scan(&user.ID, &user.Username, &user.CreatedAt, &expires)
	if err != nil {
		return nil, err
	}

	expTime, err := time.Parse(time.RFC3339, expires)
	if err != nil {
		return nil, err
	}
	if time.Now().After(expTime) {
		db.conn.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
		return nil, fmt.Errorf("session expired")
	}

	return &user, nil
}

func (db *DB) DeleteSession(sessionID string) error {
	_, err := db.conn.Exec("DELETE FROM sessions WHERE id = ?", sessionID)
	return err
}
```

The session flow:

1. **Create**: On login/register, a UUID v4 is generated (by the API layer), stored with a 30-day expiry.
2. **Validate**: `GetSession` joins `sessions` and `users` in one query to get the user. If the session has expired, it deletes the row and returns an error.
3. **Delete**: Logout removes the session row.

This is a server-side session model — the session token is sent as an HttpOnly cookie (set by the API layer), so JavaScript can't read it, preventing XSS-based session theft.

### Meal CRUD — The Save Strategy

The save operation uses a delete-and-reinsert pattern inside a transaction:

```bash
sed -n "262,288p" server/db.go
```

```output
func (db *DB) SaveMeals(userID int64, date string, meals []Meal) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete existing meals for this date
	_, err = tx.Exec("DELETE FROM meals WHERE user_id = ? AND date = ?", userID, date)
	if err != nil {
		return err
	}

	// Insert new meals
	now := time.Now().UTC().Format(time.RFC3339)
	for _, m := range meals {
		_, err := tx.Exec(
			"INSERT INTO meals (user_id, date, meal_name, content, sort_order, updated_at) VALUES (?, ?, ?, ?, ?, ?)",
			userID, date, m.MealName, m.Content, m.SortOrder, now,
		)
		if err != nil {
			return err
		}
	}

	return tx.Commit()
}
```

This is simpler than trying to diff and upsert individual meals. The frontend sends the complete state of a day's meals, the backend deletes everything for that date and reinserts. The transaction ensures atomicity — if any insert fails, the rollback restores the old data. Since the client always sends all meals for a day at once, no data can be lost.

---

## 3. API Layer — `server/api.go`

This is the HTTP routing and handler file. Let's start with how routes are registered:

```bash
sed -n "25,50p" server/api.go
```

```output
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()

	// Auth
	mux.HandleFunc("POST /api/auth/register", a.handleRegister)
	mux.HandleFunc("POST /api/auth/login", a.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", a.handleLogout)
	mux.HandleFunc("GET /api/auth/me", a.requireAuth(a.handleMe))

	// Meal types
	mux.HandleFunc("GET /api/meal-types", a.requireAuth(a.handleGetMealTypes))
	mux.HandleFunc("POST /api/meal-types", a.requireAuth(a.handleAddMealType))
	mux.HandleFunc("DELETE /api/meal-types/{name}", a.requireAuth(a.handleDeleteMealType))

	// Meals
	mux.HandleFunc("GET /api/meals", a.requireAuth(a.handleGetMeals))
	mux.HandleFunc("PUT /api/meals", a.requireAuth(a.handleSaveMeals))

	// Export
	mux.HandleFunc("GET /api/export", a.requireAuth(a.handleExport))

	// Serve static files
	mux.Handle("/", http.FileServer(http.Dir("../frontend/dist")))

	return withCORS(mux)
}
```

This uses Go 1.22's enhanced `ServeMux` pattern syntax — `"POST /api/auth/register"` means "only match POST requests to this path." The older `ServeMux` couldn't do method matching.

The route structure is clean:

| Method | Path | Auth? | Purpose |
|--------|------|-------|---------|
| POST | `/api/auth/register` | No | Create account |
| POST | `/api/auth/login` | No | Log in |
| POST | `/api/auth/logout` | No | Log out |
| GET | `/api/auth/me` | Yes | Get current user |
| GET | `/api/meal-types` | Yes | List meal types |
| POST | `/api/meal-types` | Yes | Add meal type |
| DELETE | `/api/meal-types/{name}` | Yes | Remove meal type |
| GET | `/api/meals?date=` | Yes | Get meals for a date |
| PUT | `/api/meals` | Yes | Save meals for a date |
| GET | `/api/export?format=` | Yes | Download all data |
| * | `/` | No | Serve frontend SPA |

Most routes are wrapped in `a.requireAuth(...)` — the authentication middleware. The entire mux is then wrapped in `withCORS(...)` for development cross-origin requests. Let's look at both wrappers:

### CORS Middleware

```bash
sed -n "52,67p" server/api.go
```

```output
func withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```

During development, the Vite dev server runs on port 3000 and proxies API calls to port 8080, but the browser still needs CORS headers. The middleware reflects the request `Origin` header back and allows credentials (cookies). `OPTIONS` preflight requests get a 204 with no body.

### Auth Middleware

The `requireAuth` function is a higher-order function that wraps any handler with session validation:

```bash
sed -n "69,97p" server/api.go
```

```output
type contextKey string

const userContextKey contextKey = "user"

func (a *API) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("session")
		if err != nil {
			jsonError(w, "not authenticated", http.StatusUnauthorized)
			return
		}

		user, err := a.db.GetSession(cookie.Value)
		if err != nil {
			jsonError(w, "invalid session", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), userContextKey, user)
		next(w, r.WithContext(ctx))
	}
}

func getUser(r *http.Request) *User {
	if user, ok := r.Context().Value(userContextKey).(*User); ok {
		return user
	}
	return nil
}
```

The pattern here is idiomatic Go:

1. Extract the `session` cookie from the request.
2. Look up the session in the database (which also checks expiry).
3. If valid, inject the `*User` into the request context via `context.WithValue`.
4. Downstream handlers call `getUser(r)` to retrieve it.

The custom `contextKey` type prevents key collisions with other packages that might also use string context keys.

### Registration Handler — Input Validation and Session Creation

Let's trace through a registration request:

```bash
sed -n "112,158p" server/api.go
```

```output
func (a *API) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if len(req.Username) < 3 || len(req.Username) > 64 {
		jsonError(w, "username must be 3-64 characters", http.StatusBadRequest)
		return
	}
	if len(req.Password) < 8 || len(req.Password) > 128 {
		jsonError(w, "password must be 8-128 characters", http.StatusBadRequest)
		return
	}

	user, err := a.db.CreateUser(req.Username, req.Password)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			jsonError(w, "username already taken", http.StatusConflict)
			return
		}
		jsonError(w, "failed to create user", http.StatusInternalServerError)
		return
	}

	sessionID := uuid.New().String()
	if err := a.db.CreateSession(sessionID, user.ID); err != nil {
		jsonError(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "session",
		Value:    sessionID,
		Path:     "/",
		MaxAge:   30 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})

	jsonOK(w, user)
}
```

Security-relevant details:

- **`io.LimitReader(r.Body, 1<<16)`** — caps the request body at 64KB. This prevents a malicious client from sending a multi-gigabyte request to exhaust memory.
- **Input validation** — username 3–64 chars, password 8–128 chars. These limits are checked before any database work.
- **`HttpOnly: true`** — the session cookie cannot be read by JavaScript (`document.cookie`), which defends against XSS attacks stealing session tokens.
- **`SameSite: Lax`** — the cookie is not sent on cross-site POST requests, mitigating CSRF attacks.
- **Immediate login** — after registration, a session is created and the cookie is set, so the user doesn't have to log in separately.

The login handler (`handleLogin`) follows the same pattern but calls `AuthenticateUser` instead of `CreateUser`.

### The Export Handler — Building Tabular Data

The export endpoint is the most complex handler. It builds a spreadsheet from all of a user's meal data:

```bash
sed -n "329,398p" server/api.go
```

```output
func (a *API) handleExport(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "csv"
	}

	meals, err := a.db.GetAllMeals(user.ID)
	if err != nil {
		jsonError(w, "failed to get meals", http.StatusInternalServerError)
		return
	}

	// Build export data: organize by date and collect all meal names
	type dateEntry struct {
		meals map[string]string
	}
	dates := map[string]*dateEntry{}
	mealNameSet := map[string]bool{}
	var dateOrder []string

	for _, m := range meals {
		if _, exists := dates[m.Date]; !exists {
			dates[m.Date] = &dateEntry{meals: map[string]string{}}
			dateOrder = append(dateOrder, m.Date)
		}
		dates[m.Date].meals[m.MealName] = m.Content
		mealNameSet[m.MealName] = true
	}

	// Include meal types in column order, plus any extras from data
	mealTypes, _ := a.db.GetMealTypes(user.ID)
	var mealNames []string
	added := map[string]bool{}
	for _, mt := range mealTypes {
		mealNames = append(mealNames, mt.Name)
		added[mt.Name] = true
	}
	var extraNames []string
	for name := range mealNameSet {
		if !added[name] {
			extraNames = append(extraNames, name)
		}
	}
	sort.Strings(extraNames)
	mealNames = append(mealNames, extraNames...)

	sort.Strings(dateOrder)

	// Build rows
	header := append([]string{"Date"}, mealNames...)
	header = append(header, "All Meals")

	var rows [][]string
	rows = append(rows, header)

	for _, date := range dateOrder {
		entry := dates[date]
		row := []string{date}
		var allMeals []string
		for _, name := range mealNames {
			content := entry.meals[name]
			row = append(row, content)
			if content != "" {
				allMeals = append(allMeals, name+": "+content)
			}
		}
		row = append(row, strings.Join(allMeals, " | "))
		rows = append(rows, row)
	}
```

The data transformation:

1. **Pivot** — flat `(date, meal_name, content)` rows from the database get pivoted into a map of `date → { meal_name → content }`.
2. **Column order** — current meal types come first (in their sort order), then any historical meal names that no longer exist as types get appended alphabetically. This means the export always has stable columns even if the user deleted a meal type.
3. **Summary column** — "All Meals" concatenates non-empty entries as `"Breakfast: eggs | Lunch: salad"`, giving a quick at-a-glance view of the day.

The resulting `rows` slice is then rendered into the requested format. CSV uses Go's stdlib `encoding/csv`, XLSX uses the `tealeg/xlsx` library:

```bash
sed -n "400,438p" server/api.go
```

```output
	switch format {
	case "csv":
		w.Header().Set("Content-Type", "text/csv")
		w.Header().Set("Content-Disposition", "attachment; filename=meals.csv")
		writer := csv.NewWriter(w)
		for _, row := range rows {
			writer.Write(row)
		}
		writer.Flush()

	case "xlsx":
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		w.Header().Set("Content-Disposition", "attachment; filename=meals.xlsx")

		wb := xlsx.NewFile()
		sh, err := wb.AddSheet("Meals")
		if err != nil {
			jsonError(w, "failed to create xlsx", http.StatusInternalServerError)
			return
		}

		for _, rowData := range rows {
			row := sh.AddRow()
			for _, cell := range rowData {
				row.AddCell().SetString(cell)
			}
		}

		wb.Write(w)

	case "ods":
		w.Header().Set("Content-Type", "application/vnd.oasis.opendocument.spreadsheet")
		w.Header().Set("Content-Disposition", "attachment; filename=meals.ods")
		writeODS(w, rows)

	default:
		jsonError(w, "invalid format, use csv, xlsx, or ods", http.StatusBadRequest)
	}
}
```

Each format streams directly to the `http.ResponseWriter` — no temp files. The `Content-Disposition: attachment` header tells the browser to download rather than display the content.

---

## 4. ODS Export — `server/ods.go`

ODS (OpenDocument Spreadsheet) is the format used by LibreOffice and importable by Google Sheets. It's a zip archive containing XML files. Rather than pulling in a heavy ODS library, this file builds the archive by hand:

```bash
cat -n server/ods.go
```

```output
     1	package main
     2	
     3	import (
     4		"archive/zip"
     5		"encoding/xml"
     6		"fmt"
     7		"io"
     8		"strings"
     9	)
    10	
    11	// writeODS writes an ODS (OpenDocument Spreadsheet) file to the writer.
    12	// ODS is a zip archive containing XML files.
    13	func writeODS(w io.Writer, rows [][]string) error {
    14		zw := zip.NewWriter(w)
    15		defer zw.Close()
    16	
    17		// mimetype must be first entry, stored (not compressed)
    18		mw, err := zw.CreateHeader(&zip.FileHeader{
    19			Name:   "mimetype",
    20			Method: zip.Store,
    21		})
    22		if err != nil {
    23			return err
    24		}
    25		mw.Write([]byte("application/vnd.oasis.opendocument.spreadsheet"))
    26	
    27		// META-INF/manifest.xml
    28		manifestW, err := zw.Create("META-INF/manifest.xml")
    29		if err != nil {
    30			return err
    31		}
    32		manifestW.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
    33	<manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0" manifest:version="1.2">
    34	  <manifest:file-entry manifest:media-type="application/vnd.oasis.opendocument.spreadsheet" manifest:version="1.2" manifest:full-path="/"/>
    35	  <manifest:file-entry manifest:media-type="text/xml" manifest:full-path="content.xml"/>
    36	</manifest:manifest>`))
    37	
    38		// content.xml
    39		contentW, err := zw.Create("content.xml")
    40		if err != nil {
    41			return err
    42		}
    43	
    44		contentW.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>
    45	<office:document-content
    46	  xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0"
    47	  xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0"
    48	  xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0"
    49	  office:version="1.2">
    50	<office:body>
    51	<office:spreadsheet>
    52	<table:table table:name="Meals">
    53	`))
    54	
    55		for _, row := range rows {
    56			contentW.Write([]byte("<table:table-row>\n"))
    57			for _, cell := range row {
    58				escaped := xmlEscape(cell)
    59				contentW.Write([]byte(fmt.Sprintf(`<table:table-cell office:value-type="string"><text:p>%s</text:p></table:table-cell>`+"\n", escaped)))
    60			}
    61			contentW.Write([]byte("</table:table-row>\n"))
    62		}
    63	
    64		contentW.Write([]byte(`</table:table>
    65	</office:spreadsheet>
    66	</office:body>
    67	</office:document-content>`))
    68	
    69		return nil
    70	}
    71	
    72	func xmlEscape(s string) string {
    73		var b strings.Builder
    74		xml.EscapeText(&b, []byte(s))
    75		return b.String()
    76	}
```

The ODS spec requires:

1. **`mimetype`** as the first zip entry, stored uncompressed (`zip.Store` not `zip.Deflate`). This lets file-type detectors identify the format by reading the first few bytes of the zip without decompressing.
2. **`META-INF/manifest.xml`** listing the archive contents and their media types.
3. **`content.xml`** containing the actual spreadsheet data in ODF XML.

Cell values are XML-escaped using Go's `encoding/xml.EscapeText` to handle special characters (`<`, `>`, `&`, etc.) safely. Each cell is typed as `string` since meal data is text.

This hand-rolled approach adds zero external dependencies for ODS support — just Go's stdlib `archive/zip` and `encoding/xml`.

---

## 5. Frontend Entry Chain — `index.html` → `index.tsx` → `App.tsx`

Now we cross to the client side. The entry point is a minimal HTML shell:

```bash
cat -n frontend/index.html
```

```output
     1	<!DOCTYPE html>
     2	<html lang="en">
     3	  <head>
     4	    <meta charset="UTF-8" />
     5	    <meta name="viewport" content="width=device-width, initial-scale=1.0" />
     6	    <title>Meal Tracker</title>
     7	    <link rel="icon" href="data:," />
     8	  </head>
     9	  <body>
    10	    <div id="root"></div>
    11	    <script src="/src/index.tsx" type="module"></script>
    12	  </body>
    13	</html>
```

The `<link rel="icon" href="data:,">` trick suppresses the browser's default favicon request (which would otherwise 404). Vite handles the `type="module"` script tag — in dev mode it serves the TSX directly with hot module replacement; in production it bundles it.

The SolidJS entry point mounts the app:

```bash
cat -n frontend/src/index.tsx
```

```output
     1	/* @refresh reload */
     2	import { render } from "solid-js/web";
     3	import App from "./App";
     4	
     5	render(() => <App />, document.getElementById("root")!);
```

The `/* @refresh reload */` pragma tells Vite's SolidJS plugin to do a full page reload on HMR rather than trying to preserve component state — appropriate for the root module.

`App.tsx` manages the top-level auth state:

```bash
cat -n frontend/src/App.tsx
```

```output
     1	import { createSignal, onMount, Show } from "solid-js";
     2	import { api, User } from "./api";
     3	import Auth from "./Auth";
     4	import Tracker from "./Tracker";
     5	import "./styles.css";
     6	
     7	export default function App() {
     8	  const [user, setUser] = createSignal<User | null>(null);
     9	  const [loading, setLoading] = createSignal(true);
    10	
    11	  onMount(async () => {
    12	    try {
    13	      const u = await api.me();
    14	      setUser(u);
    15	    } catch {
    16	      // not logged in
    17	    }
    18	    setLoading(false);
    19	  });
    20	
    21	  const handleLogout = async () => {
    22	    await api.logout();
    23	    setUser(null);
    24	  };
    25	
    26	  return (
    27	    <div class="app">
    28	      <Show when={!loading()} fallback={<div class="loading">Loading...</div>}>
    29	        <Show
    30	          when={user()}
    31	          fallback={<Auth onLogin={(u) => setUser(u)} />}
    32	        >
    33	          {(u) => (
    34	            <>
    35	              <header class="app-header">
    36	                <h1>Meal Tracker</h1>
    37	                <div class="header-right">
    38	                  <span class="username">{u().username}</span>
    39	                  <button class="btn btn-ghost" onClick={handleLogout}>
    40	                    Log out
    41	                  </button>
    42	                </div>
    43	              </header>
    44	              <Tracker />
    45	            </>
    46	          )}
    47	        </Show>
    48	      </Show>
    49	    </div>
    50	  );
    51	}
```

The `App` component is a state machine with three states:

1. **Loading** (lines 9, 28) — on mount, it calls `GET /api/auth/me` to check if the user has an existing session cookie. While waiting, it shows a loading indicator.
2. **Not authenticated** (line 31) — if `me()` fails (no cookie, expired session), `user()` stays `null` and the `<Auth>` component is rendered.
3. **Authenticated** (lines 33–46) — if the session is valid, it renders the header bar with the username and logout button, plus the `<Tracker>` component.

SolidJS's `<Show>` component is conditional rendering — unlike React, it doesn't re-run the entire component function on state changes. Only the reactive expressions (signal accesses like `user()` and `loading()`) trigger targeted DOM updates.

---

## 6. API Client — `frontend/src/api.ts`

All HTTP communication goes through a single `request` helper:

```bash
sed -n "1,30p" frontend/src/api.ts
```

```output
const BASE = "";

async function request<T>(
  method: string,
  path: string,
  body?: unknown
): Promise<T> {
  const opts: RequestInit = {
    method,
    credentials: "include",
    headers: { "Content-Type": "application/json" },
  };
  if (body !== undefined) {
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(BASE + path, opts);
  if (!res.ok) {
    const data = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(data.error || res.statusText);
  }
  // For file downloads, return the response itself
  if (
    res.headers.get("Content-Type")?.includes("text/csv") ||
    res.headers.get("Content-Type")?.includes("spreadsheet") ||
    res.headers.get("Content-Type")?.includes("opendocument")
  ) {
    return res as unknown as T;
  }
  return res.json();
}
```

Key design decisions:

- **`credentials: "include"`** — this tells `fetch` to send cookies with every request, which is how the session cookie reaches the server. Without this, the browser would not attach cookies to same-origin API calls made by JavaScript.
- **`BASE = ""`** — API paths are relative. In dev mode, Vite's proxy forwards `/api/*` to the Go server. In production, both the API and static files are served from the same origin.
- **Error handling** — on non-2xx responses, it tries to parse the JSON error body (`{"error": "..."}`) from the server. If the body isn't JSON, it falls back to the HTTP status text.

The `api` object then provides typed methods that mirror the backend endpoints:

```bash
sed -n "51,74p" frontend/src/api.ts
```

```output
export const api = {
  // Auth
  register: (username: string, password: string) =>
    request<User>("POST", "/api/auth/register", { username, password }),
  login: (username: string, password: string) =>
    request<User>("POST", "/api/auth/login", { username, password }),
  logout: () => request<void>("POST", "/api/auth/logout"),
  me: () => request<User>("GET", "/api/auth/me"),

  // Meal types
  getMealTypes: () => request<MealType[]>("GET", "/api/meal-types"),
  addMealType: (name: string) =>
    request<MealType>("POST", "/api/meal-types", { name }),
  deleteMealType: (name: string) =>
    request<void>("DELETE", `/api/meal-types/${encodeURIComponent(name)}`),

  // Meals
  getMeals: (date: string) => request<Meal[]>("GET", `/api/meals?date=${date}`),
  saveMeals: (date: string, meals: Meal[]) =>
    request<void>("PUT", "/api/meals", { date, meals }),

  // Export
  exportUrl: (format: string) => `/api/export?format=${format}`,
};
```

Note that `deleteMealType` uses `encodeURIComponent(name)` to safely handle meal type names with special characters in the URL path. The `exportUrl` method returns a plain URL string rather than making a fetch — the caller opens it in a new browser tab, which triggers a file download via the `Content-Disposition: attachment` header from the server.

---

## 7. Auth Component — `frontend/src/Auth.tsx`

The login/register screen is a single component that toggles between both modes:

```bash
sed -n "8,27p" frontend/src/Auth.tsx
```

```output
export default function Auth(props: Props) {
  const [isRegister, setIsRegister] = createSignal(false);
  const [username, setUsername] = createSignal("");
  const [password, setPassword] = createSignal("");
  const [error, setError] = createSignal("");
  const [submitting, setSubmitting] = createSignal(false);

  const handleSubmit = async (e: Event) => {
    e.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      const fn = isRegister() ? api.register : api.login;
      const user = await fn(username(), password());
      props.onLogin(user);
    } catch (err: any) {
      setError(err.message);
    }
    setSubmitting(false);
  };
```

The `isRegister` signal toggles the form between login and registration. The submit handler dynamically picks the right API function — `api.register` or `api.login` — based on the current mode. Both return a `User` object on success, which gets passed up to `App` via `props.onLogin(user)`, causing the app to transition from the auth screen to the tracker.

The form uses HTML5 validation attributes (`minLength`, `maxLength`, `required`) for client-side validation, plus `autocomplete` hints so password managers work correctly (`"username"`, `"current-password"`, `"new-password"`).

Error messages from the server (e.g., "username already taken", "invalid username or password") are displayed in a styled error banner above the form.

---

## 8. Tracker Component — `frontend/src/Tracker.tsx`

This is the heart of the application — 344 lines handling date navigation, meal editing, client-side caching, dirty tracking, saving, and export. Let's break it down section by section.

### Date Utilities

```bash
sed -n "12,43p" frontend/src/Tracker.tsx
```

```output
function formatDate(d: Date): string {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

function parseDate(s: string): Date {
  const [y, m, d] = s.split("-").map(Number);
  return new Date(y, m - 1, d);
}

function displayDate(s: string): string {
  const d = parseDate(s);
  return d.toLocaleDateString("en-US", {
    weekday: "short",
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}

interface MealEntry {
  meal_name: string;
  content: string;
  sort_order: number;
}

// Local cache type: date -> meals
type DayCache = Record<string, MealEntry[]>;
// Track what's been saved (from server) per date
type SavedState = Record<string, MealEntry[]>;
```

Three date helpers handle the format conversions:

- **`formatDate`** — `Date` object → `"YYYY-MM-DD"` string (for the API and date input).
- **`parseDate`** — the reverse, manually splitting to avoid timezone issues with `new Date("2025-02-25")` (which parses as UTC midnight and can shift to the wrong day in negative-UTC timezones).
- **`displayDate`** — formats for human display: `"Tue, Feb 25, 2025"`.

The two type aliases define the caching architecture:
- **`DayCache`** — maps date strings to the *current* (possibly edited) meal entries.
- **`SavedState`** — maps date strings to the *last-saved* version. Comparing these two determines what's dirty.

### State and Initialization

```bash
sed -n "45,59p" frontend/src/Tracker.tsx
```

```output
export default function Tracker() {
  const [selectedDate, setSelectedDate] = createSignal(formatDate(new Date()));
  const [mealTypes, setMealTypes] = createSignal<MealType[]>([]);
  const [cache, setCache] = createStore<DayCache>({});
  const [saved, setSaved] = createStore<SavedState>({});
  const [saving, setSaving] = createSignal(false);
  const [newMealName, setNewMealName] = createSignal("");
  const [bannerOpen, setBannerOpen] = createSignal(true);
  const [dateInput, setDateInput] = createSignal("");

  // Load meal types on mount
  onMount(async () => {
    const types = await api.getMealTypes();
    setMealTypes(types);
  });
```

SolidJS has two reactivity primitives at play here:

- **Signals** (`createSignal`) — for simple values like the selected date, the saving flag, and the new meal name input.
- **Stores** (`createStore`) — for the `cache` and `saved` objects. Stores provide deep reactivity — when you update `cache["2025-02-25"][0].content`, only the UI elements reading that specific nested value will re-render. This is critical for performance: typing in one meal's textarea shouldn't trigger a re-render of all other meals.

On mount, `mealTypes` are fetched once — these are the user's configured meal slots (Breakfast, Lunch, etc.).

### Reactive Data Loading

```bash
sed -n "61,97p" frontend/src/Tracker.tsx
```

```output
  // Load meals when date changes
  createEffect(async () => {
    const date = selectedDate();
    setDateInput(date);
    if (cache[date]) return; // Already cached

    const meals = await api.getMeals(date);
    const types = mealTypes();

    // Build entry list from meal types, merging with server data
    const entries: MealEntry[] = types.map((t, i) => {
      const existing = meals.find((m) => m.meal_name === t.name);
      return {
        meal_name: t.name,
        content: existing?.content ?? "",
        sort_order: existing?.sort_order ?? i,
      };
    });

    // Add any server meals not in current types
    for (const m of meals) {
      if (!entries.find((e) => e.meal_name === m.meal_name)) {
        entries.push({
          meal_name: m.meal_name,
          content: m.content,
          sort_order: m.sort_order,
        });
      }
    }

    entries.sort((a, b) => a.sort_order - b.sort_order);

    batch(() => {
      setCache(date, [...entries]);
      setSaved(date, entries.map((e) => ({ ...e })));
    });
  });
```

This `createEffect` automatically re-runs whenever `selectedDate()` changes (SolidJS tracks signal reads inside effects). The logic:

1. **Cache check** — if this date has already been loaded, skip the fetch. This is the "client-side caching" behavior: navigating back to a previously-viewed date is instant and preserves any unsaved edits.
2. **Fetch and merge** — get the server's saved meals for this date, then build an entry list starting from the user's meal types. This ensures all configured meal types appear as cards even if they have no saved content.
3. **Historical meals** — if the server has meals under names that no longer exist in the user's meal types (e.g., they deleted "Second Breakfast" but old data still has it), those get appended.
4. **Dual write** — both `cache` and `saved` get identical copies. `cache` will diverge as the user edits; `saved` stays frozen until the next successful save.

The `batch()` call groups both store updates into a single reactive flush, preventing intermediate renders.

### Dirty Tracking

```bash
sed -n "99,129p" frontend/src/Tracker.tsx
```

```output
  // Compute which dates have unsaved changes
  const unsavedDates = () => {
    const dates: string[] = [];
    for (const date of Object.keys(cache)) {
      if (isDirty(date)) {
        dates.push(date);
      }
    }
    dates.sort();
    return dates;
  };

  const isDirty = (date: string): boolean => {
    const current = cache[date];
    const original = saved[date];
    if (!current || !original) return !!current;
    if (current.length !== original.length) return true;
    for (let i = 0; i < current.length; i++) {
      if (
        current[i].meal_name !== original[i].meal_name ||
        current[i].content !== original[i].content
      ) {
        return true;
      }
    }
    return false;
  };

  const hasUnsaved = () => unsavedDates().length > 0;

  const currentMeals = () => cache[selectedDate()] ?? [];
```

The dirty detection is a field-by-field comparison between `cache[date]` and `saved[date]`:

- If the number of meals changed (added or removed a meal), it's dirty.
- If any meal's name or content differs from the saved version, it's dirty.

`unsavedDates()` iterates all cached dates and collects the dirty ones. Because these functions read from SolidJS stores, they're automatically reactive — the unsaved-changes banner and save button update in real time as the user types.

`hasUnsaved()` is a derived signal that enables/disables the save button.

### Mutation Functions

```bash
sed -n "131,173p" frontend/src/Tracker.tsx
```

```output
  const updateMealContent = (index: number, content: string) => {
    const date = selectedDate();
    setCache(
      produce((c) => {
        if (c[date]) {
          c[date][index].content = content;
        }
      })
    );
  };

  const removeMeal = (index: number) => {
    const date = selectedDate();
    setCache(
      produce((c) => {
        if (c[date]) {
          c[date].splice(index, 1);
          // Re-index sort_order
          c[date].forEach((m, i) => (m.sort_order = i));
        }
      })
    );
  };

  const addMeal = () => {
    const name = newMealName().trim();
    if (!name) return;
    const date = selectedDate();
    const meals = cache[date] ?? [];
    if (meals.find((m) => m.meal_name === name)) return; // Already exists

    setCache(
      produce((c) => {
        if (!c[date]) c[date] = [];
        c[date].push({
          meal_name: name,
          content: "",
          sort_order: c[date].length,
        });
      })
    );
    setNewMealName("");
  };
```

All three mutations use SolidJS's `produce` — an Immer-like API for stores that lets you write imperative mutations against a draft proxy. Under the hood, SolidJS converts these into fine-grained reactive updates.

- **`updateMealContent`** — called on every keystroke in a textarea. Only the specific `cache[date][index].content` path is updated, so only that textarea's character count re-renders.
- **`removeMeal`** — splices the entry and re-indexes `sort_order` to keep it sequential.
- **`addMeal`** — validates the name isn't empty or duplicate, then appends a new entry with empty content.

All changes stay local in `cache` — nothing touches the server until the save button is clicked.

### Save and Date Navigation

```bash
sed -n "175,213p" frontend/src/Tracker.tsx
```

```output
  const saveAll = async () => {
    setSaving(true);
    const dates = unsavedDates();
    for (const date of dates) {
      const meals = cache[date];
      if (!meals) continue;
      const toSave: Meal[] = meals.map((m, i) => ({
        date,
        meal_name: m.meal_name,
        content: m.content,
        sort_order: i,
      }));
      await api.saveMeals(date, toSave);
      setSaved(date, meals.map((e) => ({ ...e })));
    }
    setSaving(false);
  };

  const goDay = (offset: number) => {
    const d = parseDate(selectedDate());
    d.setDate(d.getDate() + offset);
    setSelectedDate(formatDate(d));
  };

  const handleDateInput = (e: Event) => {
    const val = (e.target as HTMLInputElement).value;
    setDateInput(val);
    // Validate YYYY-MM-DD format
    if (/^\d{4}-\d{2}-\d{2}$/.test(val)) {
      const d = parseDate(val);
      if (!isNaN(d.getTime())) {
        setSelectedDate(val);
      }
    }
  };

  const handleExport = (format: string) => {
    window.open(api.exportUrl(format), "_blank");
  };
```

**`saveAll`** iterates every dirty date and sends its meals to the server. After each successful save, it copies the current cache state into `saved`, which clears the dirty flag for that date. The save button shows "Saving..." and is disabled during this process.

**`goDay`** handles the arrow buttons — it parses the current date, adds or subtracts one day, and formats it back. Setting `selectedDate` triggers the `createEffect` above, which either loads from cache or fetches from the server.

**`handleDateInput`** is for manual date entry. It validates the typed value against `YYYY-MM-DD` format before updating the selected date — partial input like `"2025-02"` won't trigger a date change.

**`handleExport`** opens the export URL in a new tab. The browser handles the file download since the server sends `Content-Disposition: attachment`.

### The UI Render

```bash
sed -n "215,262p" frontend/src/Tracker.tsx
```

```output
  return (
    <main class="tracker">
      {/* Unsaved changes banner */}
      <Show when={hasUnsaved()}>
        <div class="unsaved-banner">
          <button
            class="banner-toggle"
            onClick={() => setBannerOpen(!bannerOpen())}
          >
            <span class="banner-icon">{bannerOpen() ? "▾" : "▸"}</span>
            <span>
              {unsavedDates().length} unsaved{" "}
              {unsavedDates().length === 1 ? "day" : "days"}
            </span>
          </button>
          <Show when={bannerOpen()}>
            <div class="banner-dates">
              <For each={unsavedDates()}>
                {(date) => (
                  <button
                    class="banner-date-btn"
                    classList={{ active: date === selectedDate() }}
                    onClick={() => setSelectedDate(date)}
                  >
                    {displayDate(date)}
                  </button>
                )}
              </For>
            </div>
          </Show>
        </div>
      </Show>

      {/* Date navigation */}
      <div class="date-nav">
        <button class="btn btn-icon" onClick={() => goDay(-1)} title="Previous day">
          ←
        </button>
        <input
          type="date"
          class="date-picker"
          value={dateInput()}
          onInput={handleDateInput}
        />
        <button class="btn btn-icon" onClick={() => goDay(1)} title="Next day">
          →
        </button>
      </div>
```

The UI has a clear top-to-bottom structure:

1. **Unsaved changes banner** (lines 218–246) — only appears when `hasUnsaved()` is true. The toggle button shows/hides the list of dirty dates. Each date is a clickable pill that navigates to that day. The `classList` directive conditionally adds the `active` class to highlight the currently-selected date.

2. **Date navigation** (lines 249–262) — a centered row with ← arrow, `<input type="date">`, → arrow. The native date input provides a calendar picker popup plus keyboard-typeable dates, satisfying both interaction modes.

Now the meal cards and action bar:

```bash
sed -n "264,344p" frontend/src/Tracker.tsx
```

```output
      <h2 class="date-display">{displayDate(selectedDate())}</h2>

      {/* Meal entries */}
      <div class="meals">
        <For each={currentMeals()}>
          {(meal, index) => (
            <div class="meal-card">
              <div class="meal-header">
                <h3 class="meal-name">{meal.meal_name}</h3>
                <button
                  class="btn btn-ghost btn-sm remove-btn"
                  onClick={() => removeMeal(index())}
                  title={`Remove ${meal.meal_name}`}
                >
                  ×
                </button>
              </div>
              <textarea
                class="meal-input"
                value={meal.content}
                onInput={(e) =>
                  updateMealContent(index(), e.currentTarget.value)
                }
                maxLength={4095}
                placeholder={`What did you have for ${meal.meal_name.toLowerCase()}?`}
                rows={3}
              />
              <div class="char-count">
                {meal.content.length} / 4095
              </div>
            </div>
          )}
        </For>
      </div>

      {/* Add meal */}
      <div class="add-meal">
        <input
          type="text"
          class="add-meal-input"
          value={newMealName()}
          onInput={(e) => setNewMealName(e.currentTarget.value)}
          onKeyDown={(e) => e.key === "Enter" && addMeal()}
          placeholder="Add a meal (e.g., Second Breakfast)"
          maxLength={64}
        />
        <button
          class="btn btn-secondary"
          onClick={addMeal}
          disabled={!newMealName().trim()}
        >
          + Add
        </button>
      </div>

      {/* Action bar */}
      <div class="action-bar">
        <button
          class="btn btn-primary save-btn"
          onClick={saveAll}
          disabled={!hasUnsaved() || saving()}
        >
          {saving() ? "Saving..." : "Save changes"}
        </button>

        <div class="export-group">
          <span class="export-label">Export:</span>
          <button class="btn btn-ghost btn-sm" onClick={() => handleExport("csv")}>
            CSV
          </button>
          <button class="btn btn-ghost btn-sm" onClick={() => handleExport("xlsx")}>
            XLSX
          </button>
          <button class="btn btn-ghost btn-sm" onClick={() => handleExport("ods")}>
            ODS
          </button>
        </div>
      </div>
    </main>
  );
}
```

3. **Meal cards** (lines 267–296) — `<For>` iterates `currentMeals()`, rendering a card per meal with:
   - The meal name as a header
   - A `×` button to remove the meal from this day
   - A `<textarea>` with `maxLength={4095}` for content entry
   - A character count (`"123 / 4095"`)

   The `onInput` event fires on every keystroke, calling `updateMealContent` which updates the store. Thanks to SolidJS's fine-grained reactivity, only the character count for *this specific card* re-renders — the other cards are untouched.

4. **Add meal** (lines 299–316) — a text input and button to add a custom meal type to the current day. Pressing Enter also triggers `addMeal()`.

5. **Action bar** (lines 319–342) — the save button is disabled when `!hasUnsaved() || saving()`. This means:
   - Disabled when there are no unsaved changes (nothing to save)
   - Disabled while a save is in progress (prevent double-submit)
   - Enabled only when edits exist and no save is running

   The export buttons open CSV/XLSX/ODS downloads in new tabs.

---

## 9. Styles and Design System — `frontend/src/styles.css`

The stylesheet follows Refactoring UI principles. Let's look at the design tokens:

```bash
sed -n "10,44p" frontend/src/styles.css
```

```output
:root {
  /* Red palette */
  --red-50: #fef2f2;
  --red-100: #fee2e2;
  --red-200: #fecaca;
  --red-300: #fca5a5;
  --red-400: #f87171;
  --red-500: #ef4444;
  --red-600: #dc2626;
  --red-700: #b91c1c;
  --red-800: #991b1b;
  --red-900: #7f1d1d;

  /* Neutrals */
  --gray-50: #fafafa;
  --gray-100: #f5f5f5;
  --gray-200: #e5e5e5;
  --gray-300: #d4d4d4;
  --gray-400: #a3a3a3;
  --gray-500: #737373;
  --gray-600: #525252;
  --gray-700: #404040;
  --gray-800: #262626;
  --gray-900: #171717;

  --bg: #fff;
  --text: var(--gray-800);
  --text-muted: var(--gray-500);
  --border: var(--gray-200);
  --radius: 8px;
  --radius-lg: 12px;
  --shadow-sm: 0 1px 2px rgba(0, 0, 0, 0.05);
  --shadow: 0 1px 3px rgba(0, 0, 0, 0.1), 0 1px 2px rgba(0, 0, 0, 0.06);
  --shadow-md: 0 4px 6px rgba(0, 0, 0, 0.07), 0 2px 4px rgba(0, 0, 0, 0.06);
}
```

The design system is built on two scales of CSS custom properties:

**Red palette** (10 shades, `--red-50` through `--red-900`) — following the Tailwind naming convention. Only a few are actually used, keeping the UI focused:
- `--red-600` / `--red-700` for primary actions (buttons, header)
- `--red-50` / `--red-100` for subtle backgrounds (error messages, unsaved banner, hover states)
- `--red-200` / `--red-300` / `--red-400` for borders and focus rings

**Gray palette** (10 shades) — for text, borders, and backgrounds. The app never uses raw color values in component styles — everything references these tokens.

**Semantic tokens** (`--bg`, `--text`, `--text-muted`, `--border`) provide a layer of indirection that would make a dark mode implementation straightforward.

**Shadow scale** (`--shadow-sm`, `--shadow`, `--shadow-md`) provides three levels of depth, used sparingly: cards get `--shadow-sm`, the auth card gets `--shadow-md`, and focus states elevate to `--shadow`.

A few specific styling patterns worth noting:

```bash
sed -n "389,401p" frontend/src/styles.css
```

```output
.meal-card {
  background: var(--bg);
  border: 1px solid var(--border);
  border-radius: var(--radius-lg);
  padding: 16px;
  box-shadow: var(--shadow-sm);
  transition: border-color 0.15s;
}

.meal-card:focus-within {
  border-color: var(--red-300);
  box-shadow: var(--shadow);
}
```

The `:focus-within` pseudo-class is a nice touch — when the user focuses on the textarea inside a meal card, the entire card's border changes to red and its shadow deepens. This provides a visual "active card" indicator without any JavaScript.

The focus ring pattern is consistent throughout:

```bash
grep -A3 "box-shadow: 0 0 0 3px" frontend/src/styles.css | head -16
```

```output
  box-shadow: 0 0 0 3px var(--red-100);
}

.auth-toggle {
--
  box-shadow: 0 0 0 3px var(--red-100);
}

.date-display {
--
  box-shadow: 0 0 0 3px var(--red-100);
}

.meal-input::placeholder {
--
  box-shadow: 0 0 0 3px var(--red-100);
```

Every focusable input uses the same focus ring: `border-color: var(--red-400)` with a `box-shadow: 0 0 0 3px var(--red-100)` spread. This replaces the browser's default outline with a soft red glow that matches the color scheme — consistent across the date picker, auth inputs, meal textareas, and the add-meal input.

The responsive breakpoint is minimal — a single `@media (max-width: 480px)` that tightens padding and stacks the action bar vertically on small screens:

```bash
sed -n "522,549p" frontend/src/styles.css
```

```output

@media (max-width: 480px) {
  .app-header {
    padding: 12px 16px;
  }

  .tracker {
    padding: 16px 12px 80px;
  }

  .date-nav {
    gap: 8px;
  }

  .date-picker {
    min-width: 150px;
  }

  .action-bar {
    flex-direction: column;
    align-items: stretch;
  }

  .export-group {
    justify-content: center;
  }
}
```

The responsive design is deliberately minimal — the `.tracker` container is already capped at `max-width: 680px` with `margin: 0 auto`, so the layout works well on tablets and desktops without any media queries. The mobile breakpoint just adjusts spacing.

---

## 10. Build Configuration — `frontend/vite.config.ts`

```bash
cat -n frontend/vite.config.ts
```

```output
     1	import { defineConfig } from "vite";
     2	import solidPlugin from "vite-plugin-solid";
     3	
     4	export default defineConfig({
     5	  plugins: [solidPlugin()],
     6	  server: {
     7	    port: 3000,
     8	    proxy: {
     9	      "/api": "http://localhost:8080",
    10	    },
    11	  },
    12	  build: {
    13	    target: "esnext",
    14	  },
    15	});
```

The Vite config does three things:

1. **`solidPlugin()`** — transforms SolidJS JSX into the fine-grained reactive DOM operations that make Solid fast. Unlike React's JSX→`createElement` transform, Solid's compiler produces code that directly creates and updates DOM nodes.

2. **`proxy: { "/api": "http://localhost:8080" }`** — during development, the Vite dev server (port 3000) forwards all `/api/*` requests to the Go server (port 8080). This avoids CORS issues in dev and mirrors the production setup where the Go server serves both the API and the built frontend from the same origin.

3. **`target: "esnext"`** — the production build targets the latest JavaScript features without transpilation. Since SolidJS requires modern browsers anyway, there's no reason to downlevel.

### The Production Build

Let's verify the production build still works:

```bash
cd frontend && npx vite build 2>&1
```

```output
vite v6.4.1 building for production...
transforming...
✓ 11 modules transformed.
rendering chunks...
computing gzip size...
dist/index.html                  0.44 kB │ gzip: 0.29 kB
dist/assets/index-PVN9mHOf.css   7.07 kB │ gzip: 1.85 kB
dist/assets/index-C3T4IgZW.js   22.39 kB │ gzip: 8.89 kB
✓ built in 599ms
npm notice
npm notice New major version of npm available! 10.9.4 -> 11.10.1
npm notice Changelog: https://github.com/npm/cli/releases/tag/v11.10.1
npm notice To update run: npm install -g npm@11.10.1
npm notice
```

The entire frontend compiles to three files totaling under 30KB:

- **`index.html`** (0.44KB) — the shell
- **`index-*.css`** (7.07KB, 1.85KB gzipped) — all styles
- **`index-*.js`** (22.39KB, 8.89KB gzipped) — SolidJS runtime + all application code

For comparison, a React equivalent would typically be 40–60KB gzipped for the framework alone. SolidJS's compiler-based approach produces much smaller bundles.

Let's also verify the Go backend compiles cleanly:

```bash
cd server && go build -o /dev/null . && echo "Build successful"
```

```output
Build successful
```

---

## Summary — Request Lifecycle

To tie it all together, here's the full lifecycle of saving a meal entry:

1. **User types** in a textarea → `onInput` fires → `updateMealContent` updates `cache[date][index].content` in the SolidJS store → character count re-renders, save button becomes enabled (because `isDirty` now returns true), unsaved banner appears.

2. **User clicks "Save changes"** → `saveAll` collects all dirty dates → for each date, calls `api.saveMeals(date, meals)` → `fetch("PUT /api/meals", ...)` with `credentials: "include"`.

3. **Browser sends** the request with the `session` cookie → Go's `withCORS` middleware adds response headers → `requireAuth` reads the cookie, calls `db.GetSession`, injects `*User` into context.

4. **`handleSaveMeals`** decodes the JSON body (capped at 1MB by `LimitReader`), validates content length (4095 chars), calls `db.SaveMeals`.

5. **`SaveMeals`** opens a transaction → deletes existing meals for this user+date → inserts the new set → commits.

6. **Response** returns `{"status": "ok"}` → `saveAll` copies current cache to `saved` for that date → dirty flag clears → save button disables → banner updates.

All of this happens with zero client-side routing, zero state management libraries, zero CSS frameworks, and zero ORM — just SolidJS signals/stores, Go's stdlib HTTP server, and raw SQL.

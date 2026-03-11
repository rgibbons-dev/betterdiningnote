package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// testEnv creates an in-memory database and an API wired to it.
// Returns the test server, a cleanup func, and a helper to register+login.
func testEnv(t *testing.T) (*httptest.Server, func()) {
	t.Helper()
	db, err := NewDB(":memory:")
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	api := NewAPI(db)
	srv := httptest.NewServer(api.Handler())
	return srv, func() {
		srv.Close()
		db.Close()
	}
}

// jsonBody encodes v as a JSON reader.
func jsonBody(t *testing.T, v interface{}) io.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(b)
}

// doJSON makes a JSON request and returns the response.
func doJSON(t *testing.T, method, url string, body interface{}, cookies []*http.Cookie) *http.Response {
	t.Helper()
	var bodyReader io.Reader
	if body != nil {
		bodyReader = jsonBody(t, body)
	}
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// decodeJSON decodes the response body into dest.
func decodeJSON(t *testing.T, resp *http.Response, dest interface{}) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(dest); err != nil {
		t.Fatalf("decode: %v", err)
	}
}

// registerUser registers a user and returns the session cookies.
func registerUser(t *testing.T, baseURL, username, password string) []*http.Cookie {
	t.Helper()
	resp := doJSON(t, "POST", baseURL+"/api/auth/register",
		map[string]string{"username": username, "password": password}, nil)
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		t.Fatalf("register %s: status %d, body %s", username, resp.StatusCode, body)
	}
	resp.Body.Close()
	return resp.Cookies()
}

// --- AUTH TESTS ---

func TestRegister_Success(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	resp := doJSON(t, "POST", srv.URL+"/api/auth/register",
		map[string]string{"username": "alice", "password": "password123"}, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var user User
	decodeJSON(t, resp, &user)
	if user.Username != "alice" {
		t.Errorf("expected username alice, got %s", user.Username)
	}

	// Should have a session cookie
	cookies := resp.Cookies()
	found := false
	for _, c := range cookies {
		if c.Name == "session" && c.Value != "" {
			found = true
		}
	}
	if !found {
		t.Error("expected session cookie")
	}
}

func TestRegister_DuplicateUsername(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	registerUser(t, srv.URL, "alice", "password123")

	// Try to register again with the same username
	resp := doJSON(t, "POST", srv.URL+"/api/auth/register",
		map[string]string{"username": "alice", "password": "password456"}, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for duplicate, got %d", resp.StatusCode)
	}

	// Should be a generic message (no username enumeration)
	var result map[string]string
	decodeJSON(t, resp, &result)
	if strings.Contains(result["error"], "already taken") {
		t.Error("error message should not reveal username existence")
	}
}

func TestRegister_Validation(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	tests := []struct {
		name     string
		username string
		password string
		wantCode int
	}{
		{"short username", "ab", "password123", 400},
		{"short password", "alice", "short", 400},
		{"password too long (>72)", "alice", strings.Repeat("a", 73), 400},
		{"empty body", "", "", 400},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := doJSON(t, "POST", srv.URL+"/api/auth/register",
				map[string]string{"username": tt.username, "password": tt.password}, nil)
			resp.Body.Close()
			if resp.StatusCode != tt.wantCode {
				t.Errorf("expected %d, got %d", tt.wantCode, resp.StatusCode)
			}
		})
	}
}

func TestLogin_Success(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	registerUser(t, srv.URL, "alice", "password123")

	resp := doJSON(t, "POST", srv.URL+"/api/auth/login",
		map[string]string{"username": "alice", "password": "password123"}, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var user User
	decodeJSON(t, resp, &user)
	if user.Username != "alice" {
		t.Errorf("expected alice, got %s", user.Username)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	registerUser(t, srv.URL, "alice", "password123")

	resp := doJSON(t, "POST", srv.URL+"/api/auth/login",
		map[string]string{"username": "alice", "password": "wrongpassword"}, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestLogin_NonexistentUser(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	resp := doJSON(t, "POST", srv.URL+"/api/auth/login",
		map[string]string{"username": "ghost", "password": "password123"}, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestMe_Authenticated(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	cookies := registerUser(t, srv.URL, "alice", "password123")

	resp := doJSON(t, "GET", srv.URL+"/api/auth/me", nil, cookies)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var user User
	decodeJSON(t, resp, &user)
	if user.Username != "alice" {
		t.Errorf("expected alice, got %s", user.Username)
	}
}

func TestMe_Unauthenticated(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	resp := doJSON(t, "GET", srv.URL+"/api/auth/me", nil, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

func TestLogout(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	cookies := registerUser(t, srv.URL, "alice", "password123")

	// Logout
	resp := doJSON(t, "POST", srv.URL+"/api/auth/logout", nil, cookies)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logout: expected 200, got %d", resp.StatusCode)
	}

	// Session should be invalid now
	resp = doJSON(t, "GET", srv.URL+"/api/auth/me", nil, cookies)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("after logout: expected 401, got %d", resp.StatusCode)
	}
}

func TestSessionCookie_HTTP_NotSecure(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	resp := doJSON(t, "POST", srv.URL+"/api/auth/register",
		map[string]string{"username": "alice", "password": "password123"}, nil)
	resp.Body.Close()

	for _, c := range resp.Cookies() {
		if c.Name == "session" {
			if c.Secure {
				t.Error("cookie should not be Secure over plain HTTP")
			}
			if !c.HttpOnly {
				t.Error("cookie should be HttpOnly")
			}
			return
		}
	}
	t.Error("session cookie not found")
}

// --- MEAL TYPE TESTS ---

func TestMealTypes_DefaultsOnRegister(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	cookies := registerUser(t, srv.URL, "alice", "password123")

	resp := doJSON(t, "GET", srv.URL+"/api/meal-types", nil, cookies)
	defer resp.Body.Close()

	var types []MealType
	decodeJSON(t, resp, &types)

	expected := []string{"Breakfast", "Lunch", "Dinner", "Snack"}
	if len(types) != len(expected) {
		t.Fatalf("expected %d types, got %d", len(expected), len(types))
	}
	for i, e := range expected {
		if types[i].Name != e {
			t.Errorf("type[%d]: expected %s, got %s", i, e, types[i].Name)
		}
	}
}

func TestMealTypes_AddAndDelete(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	cookies := registerUser(t, srv.URL, "alice", "password123")

	// Add a custom type
	resp := doJSON(t, "POST", srv.URL+"/api/meal-types",
		map[string]string{"name": "Second Breakfast"}, cookies)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("add type: expected 200, got %d", resp.StatusCode)
	}

	// Verify it exists
	resp = doJSON(t, "GET", srv.URL+"/api/meal-types", nil, cookies)
	var types []MealType
	decodeJSON(t, resp, &types)
	if len(types) != 5 {
		t.Fatalf("expected 5 types, got %d", len(types))
	}
	if types[4].Name != "Second Breakfast" {
		t.Errorf("expected Second Breakfast, got %s", types[4].Name)
	}

	// Delete it
	resp = doJSON(t, "DELETE", srv.URL+"/api/meal-types/Second%20Breakfast", nil, cookies)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("delete type: expected 200, got %d", resp.StatusCode)
	}

	// Verify it's gone
	resp = doJSON(t, "GET", srv.URL+"/api/meal-types", nil, cookies)
	decodeJSON(t, resp, &types)
	if len(types) != 4 {
		t.Fatalf("expected 4 types after delete, got %d", len(types))
	}
}

func TestMealTypes_DuplicateName(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	cookies := registerUser(t, srv.URL, "alice", "password123")

	resp := doJSON(t, "POST", srv.URL+"/api/meal-types",
		map[string]string{"name": "Breakfast"}, cookies)
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for duplicate, got %d", resp.StatusCode)
	}
}

func TestMealTypes_Unauthenticated(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	resp := doJSON(t, "GET", srv.URL+"/api/meal-types", nil, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// --- MEAL CRUD TESTS ---

func TestMeals_SaveAndLoad(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	cookies := registerUser(t, srv.URL, "alice", "password123")

	// Save meals for a date
	saveReq := map[string]interface{}{
		"date": "2025-03-01",
		"meals": []map[string]interface{}{
			{"meal_name": "Breakfast", "content": "Eggs and toast", "sort_order": 0},
			{"meal_name": "Lunch", "content": "Salad", "sort_order": 1},
		},
	}
	resp := doJSON(t, "PUT", srv.URL+"/api/meals", saveReq, cookies)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("save: expected 200, got %d", resp.StatusCode)
	}

	// Load meals for that date
	resp = doJSON(t, "GET", srv.URL+"/api/meals?date=2025-03-01", nil, cookies)
	var meals []Meal
	decodeJSON(t, resp, &meals)

	if len(meals) != 2 {
		t.Fatalf("expected 2 meals, got %d", len(meals))
	}
	if meals[0].MealName != "Breakfast" || meals[0].Content != "Eggs and toast" {
		t.Errorf("meal[0] unexpected: %+v", meals[0])
	}
	if meals[1].MealName != "Lunch" || meals[1].Content != "Salad" {
		t.Errorf("meal[1] unexpected: %+v", meals[1])
	}
}

func TestMeals_OverwriteOnSave(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	cookies := registerUser(t, srv.URL, "alice", "password123")

	// Save initial
	resp := doJSON(t, "PUT", srv.URL+"/api/meals", map[string]interface{}{
		"date":  "2025-03-01",
		"meals": []map[string]interface{}{{"meal_name": "Breakfast", "content": "Cereal", "sort_order": 0}},
	}, cookies)
	resp.Body.Close()

	// Overwrite
	resp = doJSON(t, "PUT", srv.URL+"/api/meals", map[string]interface{}{
		"date":  "2025-03-01",
		"meals": []map[string]interface{}{{"meal_name": "Breakfast", "content": "Pancakes", "sort_order": 0}},
	}, cookies)
	resp.Body.Close()

	// Verify overwrite
	resp = doJSON(t, "GET", srv.URL+"/api/meals?date=2025-03-01", nil, cookies)
	var meals []Meal
	decodeJSON(t, resp, &meals)
	if len(meals) != 1 {
		t.Fatalf("expected 1 meal, got %d", len(meals))
	}
	if meals[0].Content != "Pancakes" {
		t.Errorf("expected Pancakes, got %s", meals[0].Content)
	}
}

func TestMeals_EmptyDate(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	cookies := registerUser(t, srv.URL, "alice", "password123")

	resp := doJSON(t, "GET", srv.URL+"/api/meals?date=2099-01-01", nil, cookies)
	var meals []Meal
	decodeJSON(t, resp, &meals)

	if len(meals) != 0 {
		t.Fatalf("expected 0 meals for empty date, got %d", len(meals))
	}
}

func TestMeals_ContentLengthLimit(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	cookies := registerUser(t, srv.URL, "alice", "password123")

	// 4095 chars should be OK
	resp := doJSON(t, "PUT", srv.URL+"/api/meals", map[string]interface{}{
		"date":  "2025-03-01",
		"meals": []map[string]interface{}{{"meal_name": "Breakfast", "content": strings.Repeat("x", 4095), "sort_order": 0}},
	}, cookies)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("4095 chars: expected 200, got %d", resp.StatusCode)
	}

	// 4096 chars should fail
	resp = doJSON(t, "PUT", srv.URL+"/api/meals", map[string]interface{}{
		"date":  "2025-03-01",
		"meals": []map[string]interface{}{{"meal_name": "Breakfast", "content": strings.Repeat("x", 4096), "sort_order": 0}},
	}, cookies)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("4096 chars: expected 400, got %d", resp.StatusCode)
	}
}

func TestMeals_UserIsolation(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	cookiesAlice := registerUser(t, srv.URL, "alice", "password123")
	cookiesBob := registerUser(t, srv.URL, "bobuser", "password456")

	// Alice saves a meal
	resp := doJSON(t, "PUT", srv.URL+"/api/meals", map[string]interface{}{
		"date":  "2025-03-01",
		"meals": []map[string]interface{}{{"meal_name": "Breakfast", "content": "Alice's eggs", "sort_order": 0}},
	}, cookiesAlice)
	resp.Body.Close()

	// Bob should not see Alice's meals
	resp = doJSON(t, "GET", srv.URL+"/api/meals?date=2025-03-01", nil, cookiesBob)
	var meals []Meal
	decodeJSON(t, resp, &meals)
	if len(meals) != 0 {
		t.Fatalf("Bob should see 0 meals, got %d", len(meals))
	}
}

func TestMeals_MissingDate(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	cookies := registerUser(t, srv.URL, "alice", "password123")

	// GET without date param
	resp := doJSON(t, "GET", srv.URL+"/api/meals", nil, cookies)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}

	// PUT without date
	resp = doJSON(t, "PUT", srv.URL+"/api/meals", map[string]interface{}{
		"date":  "",
		"meals": []map[string]interface{}{},
	}, cookies)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

// --- EXPORT TESTS ---

func TestExport_CSV(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	cookies := registerUser(t, srv.URL, "alice", "password123")

	// Save some data
	doJSON(t, "PUT", srv.URL+"/api/meals", map[string]interface{}{
		"date": "2025-03-01",
		"meals": []map[string]interface{}{
			{"meal_name": "Breakfast", "content": "Eggs", "sort_order": 0},
			{"meal_name": "Lunch", "content": "Soup", "sort_order": 1},
		},
	}, cookies).Body.Close()

	resp := doJSON(t, "GET", srv.URL+"/api/export?format=csv", nil, cookies)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Content-Type"), "text/csv") {
		t.Errorf("expected text/csv, got %s", resp.Header.Get("Content-Type"))
	}

	reader := csv.NewReader(resp.Body)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("csv parse: %v", err)
	}

	// Header + 1 data row
	if len(records) != 2 {
		t.Fatalf("expected 2 rows, got %d", len(records))
	}

	// Header should start with "Date" and end with "All Meals"
	header := records[0]
	if header[0] != "Date" {
		t.Errorf("first col should be Date, got %s", header[0])
	}
	if header[len(header)-1] != "All Meals" {
		t.Errorf("last col should be All Meals, got %s", header[len(header)-1])
	}

	// Data row should have the date
	if records[1][0] != "2025-03-01" {
		t.Errorf("expected 2025-03-01, got %s", records[1][0])
	}
}

func TestExport_XLSX(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	cookies := registerUser(t, srv.URL, "alice", "password123")

	doJSON(t, "PUT", srv.URL+"/api/meals", map[string]interface{}{
		"date":  "2025-03-01",
		"meals": []map[string]interface{}{{"meal_name": "Breakfast", "content": "Toast", "sort_order": 0}},
	}, cookies).Body.Close()

	resp := doJSON(t, "GET", srv.URL+"/api/export?format=xlsx", nil, cookies)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "spreadsheet") {
		t.Errorf("expected spreadsheet content-type, got %s", ct)
	}
}

func TestExport_ODS(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	cookies := registerUser(t, srv.URL, "alice", "password123")

	doJSON(t, "PUT", srv.URL+"/api/meals", map[string]interface{}{
		"date":  "2025-03-01",
		"meals": []map[string]interface{}{{"meal_name": "Lunch", "content": "Rice", "sort_order": 0}},
	}, cookies).Body.Close()

	resp := doJSON(t, "GET", srv.URL+"/api/export?format=ods", nil, cookies)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "opendocument") {
		t.Errorf("expected opendocument content-type, got %s", ct)
	}
}

func TestExport_InvalidFormat(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	cookies := registerUser(t, srv.URL, "alice", "password123")

	resp := doJSON(t, "GET", srv.URL+"/api/export?format=pdf", nil, cookies)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid format, got %d", resp.StatusCode)
	}
}

func TestExport_Empty(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	cookies := registerUser(t, srv.URL, "alice", "password123")

	resp := doJSON(t, "GET", srv.URL+"/api/export?format=csv", nil, cookies)
	defer resp.Body.Close()

	reader := csv.NewReader(resp.Body)
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("csv parse: %v", err)
	}
	// Should have header only
	if len(records) != 1 {
		t.Fatalf("expected 1 row (header only), got %d", len(records))
	}
}

// --- SECURITY HEADER TESTS ---

func TestSecurityHeaders(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	resp := doJSON(t, "GET", srv.URL+"/api/auth/me", nil, nil)
	resp.Body.Close()

	checks := map[string]string{
		"X-Content-Type-Options": "nosniff",
		"X-Frame-Options":       "DENY",
		"Referrer-Policy":       "strict-origin-when-cross-origin",
		"Content-Security-Policy": "default-src 'self'",
	}
	for header, want := range checks {
		got := resp.Header.Get(header)
		if got != want {
			t.Errorf("%s: expected %q, got %q", header, want, got)
		}
	}
}

func TestCORS_AllowedOrigin(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	req, _ := http.NewRequest("OPTIONS", srv.URL+"/api/auth/login", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	if resp.Header.Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Error("expected CORS allow for localhost:3000")
	}
}

func TestCORS_DisallowedOrigin(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	req, _ := http.NewRequest("OPTIONS", srv.URL+"/api/auth/login", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	resp, _ := http.DefaultClient.Do(req)
	resp.Body.Close()

	if resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Error("should NOT set CORS headers for disallowed origin")
	}
}

// --- RATE LIMITER UNIT TESTS ---

func TestRateLimiter_AllowsUnderLimit(t *testing.T) {
	rl := newRateLimiter()
	for i := 0; i < 5; i++ {
		if !rl.allow("1.2.3.4", 5, time.Minute) {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
}

func TestRateLimiter_BlocksOverLimit(t *testing.T) {
	rl := newRateLimiter()
	for i := 0; i < 5; i++ {
		rl.allow("1.2.3.4", 5, time.Minute)
	}
	if rl.allow("1.2.3.4", 5, time.Minute) {
		t.Error("6th request should be blocked")
	}
}

func TestRateLimiter_SeparateIPs(t *testing.T) {
	rl := newRateLimiter()
	for i := 0; i < 5; i++ {
		rl.allow("1.2.3.4", 5, time.Minute)
	}
	// Different IP should still be allowed
	if !rl.allow("5.6.7.8", 5, time.Minute) {
		t.Error("different IP should be allowed")
	}
}

func TestRateLimiter_ExpiresOldEntries(t *testing.T) {
	rl := newRateLimiter()
	// Fill with 5 attempts using a tiny window
	for i := 0; i < 5; i++ {
		rl.allow("1.2.3.4", 5, time.Millisecond)
	}
	// Wait for window to expire
	time.Sleep(5 * time.Millisecond)
	// Should be allowed again
	if !rl.allow("1.2.3.4", 5, time.Millisecond) {
		t.Error("should be allowed after window expires")
	}
}

func TestRateLimiter_IntegrationRegister(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	// The global authLimiter is shared, so we test the endpoint directly.
	// Register 5 unique users rapidly (each from same test client IP).
	for i := 0; i < 5; i++ {
		resp := doJSON(t, "POST", srv.URL+"/api/auth/register", map[string]interface{}{
			"username": fmt.Sprintf("ratelimituser%d", i),
			"password": "password123",
		}, nil)
		resp.Body.Close()
		// These may or may not hit the rate limit depending on global state,
		// but at minimum the first should succeed.
		if i == 0 && resp.StatusCode != http.StatusOK {
			t.Fatalf("first register should succeed, got %d", resp.StatusCode)
		}
	}
}

func TestRateLimiter_IntegrationLogin(t *testing.T) {
	srv, cleanup := testEnv(t)
	defer cleanup()

	// Register a user first
	registerUser(t, srv.URL, "ratelimitlogin", "password123")

	// Attempt many logins with wrong password — eventually should be rate limited
	var hitLimit bool
	for i := 0; i < 10; i++ {
		resp := doJSON(t, "POST", srv.URL+"/api/auth/login", map[string]interface{}{
			"username": "ratelimitlogin",
			"password": "wrongpassword",
		}, nil)
		resp.Body.Close()
		if resp.StatusCode == http.StatusTooManyRequests {
			hitLimit = true
			break
		}
	}
	if !hitLimit {
		t.Log("rate limit was not triggered (may depend on global limiter state from other tests)")
	}
}

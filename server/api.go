package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/tealeg/xlsx/v3"
)

type API struct {
	db *DB
}

func NewAPI(db *DB) *API {
	return &API{db: db}
}

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

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func jsonOK(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(data)
}

// Auth handlers

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

func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	user, err := a.db.AuthenticateUser(req.Username, req.Password)
	if err != nil {
		jsonError(w, "invalid username or password", http.StatusUnauthorized)
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

func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("session")
	if err == nil {
		a.db.DeleteSession(cookie.Value)
	}

	http.SetCookie(w, &http.Cookie{
		Name:   "session",
		Value:  "",
		Path:   "/",
		MaxAge: -1,
	})

	jsonOK(w, map[string]string{"status": "ok"})
}

func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	jsonOK(w, user)
}

// Meal type handlers

func (a *API) handleGetMealTypes(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	types, err := a.db.GetMealTypes(user.ID)
	if err != nil {
		jsonError(w, "failed to get meal types", http.StatusInternalServerError)
		return
	}
	if types == nil {
		types = []MealType{}
	}
	jsonOK(w, types)
}

func (a *API) handleAddMealType(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 64 {
		jsonError(w, "meal type name must be 1-64 characters", http.StatusBadRequest)
		return
	}

	mt, err := a.db.AddMealType(user.ID, req.Name)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			jsonError(w, "meal type already exists", http.StatusConflict)
			return
		}
		jsonError(w, "failed to add meal type", http.StatusInternalServerError)
		return
	}

	jsonOK(w, mt)
}

func (a *API) handleDeleteMealType(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	name := r.PathValue("name")
	if name == "" {
		jsonError(w, "name required", http.StatusBadRequest)
		return
	}

	if err := a.db.DeleteMealType(user.ID, name); err != nil {
		jsonError(w, "failed to delete meal type", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]string{"status": "ok"})
}

// Meal handlers

func (a *API) handleGetMeals(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	date := r.URL.Query().Get("date")
	if date == "" {
		jsonError(w, "date parameter required", http.StatusBadRequest)
		return
	}

	meals, err := a.db.GetMealsForDate(user.ID, date)
	if err != nil {
		jsonError(w, "failed to get meals", http.StatusInternalServerError)
		return
	}
	if meals == nil {
		meals = []Meal{}
	}
	jsonOK(w, meals)
}

func (a *API) handleSaveMeals(w http.ResponseWriter, r *http.Request) {
	user := getUser(r)
	var req struct {
		Date  string `json:"date"`
		Meals []Meal `json:"meals"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}

	if req.Date == "" {
		jsonError(w, "date required", http.StatusBadRequest)
		return
	}

	for _, m := range req.Meals {
		if len(m.Content) > 4095 {
			jsonError(w, fmt.Sprintf("meal '%s' content exceeds 4095 characters", m.MealName), http.StatusBadRequest)
			return
		}
	}

	if err := a.db.SaveMeals(user.ID, req.Date, req.Meals); err != nil {
		jsonError(w, "failed to save meals", http.StatusInternalServerError)
		return
	}

	jsonOK(w, map[string]string{"status": "ok"})
}

// Export handler

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

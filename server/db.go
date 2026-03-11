package main

import (
	"database/sql"
	"fmt"
	"log"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

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

func (db *DB) Close() error {
	return db.conn.Close()
}

// StartSessionCleanup runs a background goroutine that deletes expired
// sessions every hour, preventing unbounded table growth.
func (db *DB) StartSessionCleanup() {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			result, err := db.conn.Exec(
				"DELETE FROM sessions WHERE expires_at < ?",
				time.Now().UTC().Format(time.RFC3339),
			)
			if err != nil {
				log.Printf("session cleanup error: %v", err)
				continue
			}
			if n, _ := result.RowsAffected(); n > 0 {
				log.Printf("cleaned up %d expired sessions", n)
			}
		}
	}()
}

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

// Auth

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

// Sessions

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

// Meal Types

func (db *DB) GetMealTypes(userID int64) ([]MealType, error) {
	rows, err := db.conn.Query(
		"SELECT id, user_id, name, sort_order FROM meal_types WHERE user_id = ? ORDER BY sort_order",
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var types []MealType
	for rows.Next() {
		var mt MealType
		if err := rows.Scan(&mt.ID, &mt.UserID, &mt.Name, &mt.SortOrder); err != nil {
			return nil, err
		}
		types = append(types, mt)
	}
	return types, nil
}

func (db *DB) AddMealType(userID int64, name string) (*MealType, error) {
	var maxOrder int
	db.conn.QueryRow(
		"SELECT COALESCE(MAX(sort_order), -1) FROM meal_types WHERE user_id = ?",
		userID,
	).Scan(&maxOrder)

	result, err := db.conn.Exec(
		"INSERT INTO meal_types (user_id, name, sort_order) VALUES (?, ?, ?)",
		userID, name, maxOrder+1,
	)
	if err != nil {
		return nil, err
	}

	id, _ := result.LastInsertId()
	return &MealType{ID: id, UserID: userID, Name: name, SortOrder: maxOrder + 1}, nil
}

func (db *DB) DeleteMealType(userID int64, name string) error {
	_, err := db.conn.Exec(
		"DELETE FROM meal_types WHERE user_id = ? AND name = ?",
		userID, name,
	)
	return err
}

// Meals

func (db *DB) GetMealsForDate(userID int64, date string) ([]Meal, error) {
	rows, err := db.conn.Query(
		"SELECT id, user_id, date, meal_name, content, sort_order FROM meals WHERE user_id = ? AND date = ? ORDER BY sort_order",
		userID, date,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var meals []Meal
	for rows.Next() {
		var m Meal
		if err := rows.Scan(&m.ID, &m.UserID, &m.Date, &m.MealName, &m.Content, &m.SortOrder); err != nil {
			return nil, err
		}
		meals = append(meals, m)
	}
	return meals, nil
}

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

// Export

func (db *DB) GetAllMeals(userID int64) ([]Meal, error) {
	rows, err := db.conn.Query(
		"SELECT id, user_id, date, meal_name, content, sort_order FROM meals WHERE user_id = ? ORDER BY date, sort_order",
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var meals []Meal
	for rows.Next() {
		var m Meal
		if err := rows.Scan(&m.ID, &m.UserID, &m.Date, &m.MealName, &m.Content, &m.SortOrder); err != nil {
			return nil, err
		}
		meals = append(meals, m)
	}
	return meals, nil
}

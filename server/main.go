package main

import (
	"log"
	"net/http"
	"os"
)

func main() {
	dbPath := "mealtracker.db"
	if p := os.Getenv("DB_PATH"); p != "" {
		dbPath = p
	}

	db, err := NewDB(dbPath)
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	api := NewAPI(db)

	addr := ":8080"
	if a := os.Getenv("ADDR"); a != "" {
		addr = a
	}

	log.Printf("server listening on %s", addr)
	if err := http.ListenAndServe(addr, api.Handler()); err != nil {
		log.Fatal(err)
	}
}

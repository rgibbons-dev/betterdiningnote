# Meal Tracker

A simple meal tracking web app. SolidJS frontend, Go + SQLite backend.

## Prerequisites

- Go 1.22+
- Node.js 20+
- GCC (for sqlite3 CGo driver)

## Setup

### Frontend

```sh
cd frontend
npm install
npm run build
```

### Backend

```sh
cd server
go build -o mealtracker .
./mealtracker
```

The server starts on `http://localhost:8080` and serves the built frontend.

### Development

Run the frontend dev server with API proxy:

```sh
cd frontend
npm run dev    # http://localhost:3000, proxies /api to :8080
```

Run the Go server separately:

```sh
cd server
go run .
```

## Features

- User authentication (register/login with bcrypt-hashed passwords)
- Date-based meal tracking with date picker and day navigation arrows
- 4 default meal types: Breakfast, Lunch, Dinner, Snack
- Add custom meal types or remove existing ones per day
- Text entries up to 4095 characters per meal
- Client-side caching with explicit save button
- Collapsible banner showing days with unsaved changes
- Export all data as CSV, XLSX, or ODS

## Architecture

```
server/
  main.go     - Entry point
  db.go       - SQLite database layer
  api.go      - HTTP API handlers
  ods.go      - ODS export writer

frontend/
  src/
    index.tsx   - Entry point
    App.tsx     - Root component with auth state
    Auth.tsx    - Login/register form
    Tracker.tsx - Main meal tracker with date nav, meals, export
    api.ts      - API client
    styles.css  - Red-oriented styling
```

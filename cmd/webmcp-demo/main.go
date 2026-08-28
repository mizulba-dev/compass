// Command webmcp-demo serves the Canvas API and the built React SPA from a
// single HTTP server, so the browser page and its WebMCP tools share one
// origin (required for the credentials: "same-origin" fetches in
// web/src/webmcp).
package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/mizulba-dev/webmcp-demo/internal/api"
	"github.com/mizulba-dev/webmcp-demo/internal/store"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	staticDir := os.Getenv("STATIC_DIR")
	if staticDir == "" {
		staticDir = "web/dist"
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("connect to database: %v", err)
	}

	s, err := store.New(ctx, db)
	if err != nil {
		log.Fatalf("apply schema: %v", err)
	}

	mux := http.NewServeMux()
	_, apiHandler := api.New(s)
	mux.Handle("/api/", apiHandler)

	mux.Handle("/robots.txt", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
	}))

	mux.Handle("/", spaHandler(staticDir))

	addr := ":" + port
	log.Printf("webmcp-demo listening on %s (static dir: %s)", addr, staticDir)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}

// spaHandler serves the built SPA, falling back to index.html for any path
// that isn't a real file so client-side routing (canvas share links) works.
//
// Cache-Control matters here beyond performance: index.html has no
// Cache-Control by default (only Last-Modified), so browsers may
// heuristically cache it — including in embedded/in-app browsers with their
// own caching quirks — and keep serving a stale bundle whose WebMCP tool
// registration doesn't match what's actually deployed. index.html is
// explicitly no-cache; /assets/* (Vite's content-hashed filenames) are the
// opposite — safe to cache forever, since a changed file gets a new name.
func spaHandler(dir string) http.Handler {
	fileServer := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := dir + r.URL.Path
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			if info, err := os.Stat(path); err == nil && !info.IsDir() {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				fileServer.ServeHTTP(w, r)
				return
			}
		} else if info, err := os.Stat(path); err == nil && !info.IsDir() {
			fileServer.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, dir+"/index.html")
	})
}

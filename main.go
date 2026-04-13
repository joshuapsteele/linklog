package main

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

//go:embed templates/*.html
var templateFS embed.FS

//go:embed static/*
var staticFS embed.FS

func main() {
	// Configuration from environment.
	token := os.Getenv("LINKLOG_API_TOKEN")
	if token == "" {
		slog.Error("LINKLOG_API_TOKEN is required")
		os.Exit(1)
	}

	dbPath := envOr("LINKLOG_DB_PATH", "./linklog.db")
	port := envOr("LINKLOG_PORT", "8080")
	baseURL := envOr("LINKLOG_BASE_URL", "https://links.joshuapsteele.com")

	// Open database.
	db, err := OpenDB(dbPath)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Parse templates. Each page template is parsed together with the base
	// template so that block definitions don't clobber each other.
	funcMap := template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
	}

	pages := []string{"feed.html", "single.html", "tag.html"}
	templates := make(map[string]*template.Template, len(pages))
	for _, page := range pages {
		t, err := template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/base.html", "templates/"+page)
		if err != nil {
			slog.Error("failed to parse template", "page", page, "error", err)
			os.Exit(1)
		}
		templates[page] = t
	}

	srv := &Server{
		db:        db,
		templates: templates,
		baseURL:   baseURL,
		token:     token,
	}

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Compress(5))

	// Static files (embedded).
	staticSub, err := fs.Sub(staticFS, "static")
	if err != nil {
		slog.Error("failed to create static sub-filesystem", "error", err)
		os.Exit(1)
	}
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// Public pages.
	r.Get("/", srv.pageFeed)
	r.Get("/link/{id}", srv.pageSingle)
	r.Get("/tag/{tag}", srv.pageTag)
	r.Get("/feed.xml", srv.feedRSS)
	r.Get("/feed.json", srv.feedJSON)

	// Authenticated API.
	r.Route("/api", func(r chi.Router) {
		r.Use(srv.requireToken)
		r.Post("/links", srv.apiCreateLink)
		r.Get("/links", srv.apiListLinks)
		r.Patch("/links/{id}", srv.apiUpdateLink)
		r.Delete("/links/{id}", srv.apiDeleteLink)
	})

	addr := fmt.Sprintf(":%s", port)
	slog.Info("starting server", "addr", addr, "db", dbPath)
	if err := http.ListenAndServe(addr, r); err != nil {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

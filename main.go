package main

import (
	"embed"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"

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

	// Public page templates — each parsed with the shared base.
	publicPages := []string{"feed.html", "single.html", "tag.html"}
	// Admin page templates — each parsed with the admin base.
	adminPages := []string{"admin_login.html", "admin_index.html", "admin_edit.html", "admin_new.html"}

	templates := make(map[string]*template.Template, len(publicPages)+len(adminPages))

	for _, page := range publicPages {
		t, err := template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/base.html", "templates/"+page)
		if err != nil {
			slog.Error("failed to parse template", "page", page, "error", err)
			os.Exit(1)
		}
		templates[page] = t
	}

	for _, page := range adminPages {
		t, err := template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/admin_base.html", "templates/"+page)
		if err != nil {
			slog.Error("failed to parse admin template", "page", page, "error", err)
			os.Exit(1)
		}
		templates[page] = t
	}

	srv := &Server{
		db:        db,
		templates: templates,
		baseURL:   baseURL,
		token:     token,
		secure:    strings.HasPrefix(baseURL, "https://"),
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

	// Admin UI. Login is unauthenticated; everything else requires a valid session cookie.
	r.Route("/admin", func(r chi.Router) {
		r.Get("/login", srv.adminGetLogin)
		r.Post("/login", srv.adminPostLogin)

		r.Group(func(r chi.Router) {
			r.Use(srv.adminRequireAuth)
			r.Get("/", srv.adminIndex)
			r.Post("/logout", srv.adminPostLogout)
			r.Get("/links/new", srv.adminGetNew)
			r.Post("/links/new", srv.adminPostNew)
			r.Get("/links/{id}/edit", srv.adminGetEdit)
			r.Post("/links/{id}/edit", srv.adminPostEdit)
			r.Post("/links/{id}/delete", srv.adminPostDelete)
			r.Post("/links/{id}/webmention", srv.adminPostWebmention)
		})
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

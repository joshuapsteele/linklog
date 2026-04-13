package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const perPage = 20

// Server holds the dependencies shared across all handlers.
type Server struct {
	db        *DB
	templates map[string]*template.Template
	baseURL   string
	token     string
	secure    bool // true when serving over HTTPS; controls cookie Secure flag
}

// --- API Handlers ---

// apiCreateLink handles POST /api/links.
func (s *Server) apiCreateLink(w http.ResponseWriter, r *http.Request) {
	var req CreateLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	req.URL = strings.TrimSpace(req.URL)
	if req.URL == "" || (!strings.HasPrefix(req.URL, "http://") && !strings.HasPrefix(req.URL, "https://")) {
		http.Error(w, `{"error":"url is required and must start with http:// or https://"}`, http.StatusBadRequest)
		return
	}

	// Fetch page metadata in the background of this request.
	meta := FetchPageMeta(req.URL)

	link, err := s.db.InsertLink(req.URL, meta.Title, strings.TrimSpace(req.Commentary), strings.TrimSpace(req.Tags))
	if err != nil {
		slog.Error("failed to insert link", "error", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(link)
}

// apiDeleteLink handles DELETE /api/links/{id}.
func (s *Server) apiDeleteLink(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}

	deleted, err := s.db.DeleteLink(id)
	if err != nil {
		slog.Error("failed to delete link", "error", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	if !deleted {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// apiUpdateLink handles PATCH /api/links/{id}.
func (s *Server) apiUpdateLink(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Error(w, `{"error":"invalid id"}`, http.StatusBadRequest)
		return
	}

	var req UpdateLinkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	link, err := s.db.UpdateLink(id, req)
	if err != nil {
		slog.Error("failed to update link", "error", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}
	if link == nil {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(link)
}

// apiListLinks handles GET /api/links.
func (s *Server) apiListLinks(w http.ResponseWriter, r *http.Request) {
	f := LinkFilter{
		Tag:   r.URL.Query().Get("tag"),
		Limit: 100,
	}

	if pub := r.URL.Query().Get("published"); pub != "" {
		b := pub != "false"
		f.Published = &b
	}
	if lim := r.URL.Query().Get("limit"); lim != "" {
		if n, err := strconv.Atoi(lim); err == nil && n > 0 {
			f.Limit = n
		}
	}

	links, err := s.db.ListLinks(f)
	if err != nil {
		slog.Error("failed to list links", "error", err)
		http.Error(w, `{"error":"internal server error"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(links)
}

// --- Public Page Handlers ---

// feedData holds the template context for the main feed and tag pages.
type feedData struct {
	Links      []Link
	Tag        string
	Page       int
	TotalPages int
	HasNewer   bool
	HasOlder   bool
	BaseURL    string
	PagePath   string // e.g. "/" or "/tag/go"
}

// pageFeed handles GET / (main feed).
func (s *Server) pageFeed(w http.ResponseWriter, r *http.Request) {
	page := pageNum(r)
	pub := true
	f := LinkFilter{Published: &pub, Limit: perPage, Offset: (page - 1) * perPage}

	links, err := s.db.ListLinks(f)
	if err != nil {
		slog.Error("failed to list links", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	total, err := s.db.CountLinks(LinkFilter{Published: &pub})
	if err != nil {
		slog.Error("failed to count links", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	totalPages := int(math.Ceil(float64(total) / float64(perPage)))
	if totalPages < 1 {
		totalPages = 1
	}

	data := feedData{
		Links:      links,
		Page:       page,
		TotalPages: totalPages,
		HasNewer:   page > 1,
		HasOlder:   page < totalPages,
		BaseURL:    s.baseURL,
		PagePath:   "/",
	}

	s.render(w, "feed.html", data)
}

// pageTag handles GET /tag/{tag}.
func (s *Server) pageTag(w http.ResponseWriter, r *http.Request) {
	tag := chi.URLParam(r, "tag")
	if tag == "" {
		http.NotFound(w, r)
		return
	}

	page := pageNum(r)
	pub := true
	f := LinkFilter{Tag: tag, Published: &pub, Limit: perPage, Offset: (page - 1) * perPage}

	links, err := s.db.ListLinks(f)
	if err != nil {
		slog.Error("failed to list links", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	total, err := s.db.CountLinks(LinkFilter{Tag: tag, Published: &pub})
	if err != nil {
		slog.Error("failed to count links", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	totalPages := int(math.Ceil(float64(total) / float64(perPage)))
	if totalPages < 1 {
		totalPages = 1
	}

	data := feedData{
		Links:      links,
		Tag:        tag,
		Page:       page,
		TotalPages: totalPages,
		HasNewer:   page > 1,
		HasOlder:   page < totalPages,
		BaseURL:    s.baseURL,
		PagePath:   "/tag/" + tag,
	}

	s.render(w, "tag.html", data)
}

// singleData holds the template context for a single link page.
type singleData struct {
	Link    *Link
	BaseURL string
}

// pageSingle handles GET /link/{id}.
func (s *Server) pageSingle(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	link, err := s.db.GetLink(id)
	if err != nil {
		slog.Error("failed to get link", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if link == nil || !link.Published {
		http.NotFound(w, r)
		return
	}

	s.render(w, "single.html", singleData{Link: link, BaseURL: s.baseURL})
}

// --- RSS Feed ---

type rssChannel struct {
	XMLName       xml.Name  `xml:"rss"`
	Version       string    `xml:"version,attr"`
	AtomNamespace string    `xml:"xmlns:atom,attr"`
	Channel       rssFeed   `xml:"channel"`
}

type rssFeed struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	AtomLink    atomLink  `xml:"atom:link"`
	Items       []rssItem `xml:"item"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	Description string `xml:"description"`
	GUID        string `xml:"guid"`
	PubDate     string `xml:"pubDate"`
}

// feedRSS handles GET /feed.xml.
func (s *Server) feedRSS(w http.ResponseWriter, r *http.Request) {
	pub := true
	links, err := s.db.ListLinks(LinkFilter{Published: &pub, Limit: 50})
	if err != nil {
		slog.Error("failed to list links for RSS", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	items := make([]rssItem, 0, len(links))
	for _, l := range links {
		desc := l.Commentary
		if desc == "" {
			desc = l.URL
		}
		items = append(items, rssItem{
			Title:       l.Title,
			Link:        l.URL,
			Description: desc,
			GUID:        fmt.Sprintf("%s/link/%d", s.baseURL, l.ID),
			PubDate:     l.CreatedAt.Format(time.RFC1123Z),
		})
	}

	feed := rssChannel{
		Version:       "2.0",
		AtomNamespace: "http://www.w3.org/2005/Atom",
		Channel: rssFeed{
			Title:       "LinkLog — Joshua Steele",
			Link:        s.baseURL,
			Description: "Interesting links with short commentary.",
			AtomLink: atomLink{
				Href: s.baseURL + "/feed.xml",
				Rel:  "self",
				Type: "application/rss+xml",
			},
			Items: items,
		},
	}

	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Write([]byte(xml.Header))
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	enc.Encode(feed)
}

// --- JSON Feed ---

type jsonFeed struct {
	Version     string         `json:"version"`
	Title       string         `json:"title"`
	HomePageURL string         `json:"home_page_url"`
	FeedURL     string         `json:"feed_url"`
	Description string         `json:"description"`
	Items       []jsonFeedItem `json:"items"`
}

type jsonFeedItem struct {
	ID            string `json:"id"`
	URL           string `json:"url"`
	ExternalURL   string `json:"external_url"`
	Title         string `json:"title"`
	ContentHTML   string `json:"content_html"`
	DatePublished string `json:"date_published"`
}

// feedJSON handles GET /feed.json.
func (s *Server) feedJSON(w http.ResponseWriter, r *http.Request) {
	pub := true
	links, err := s.db.ListLinks(LinkFilter{Published: &pub, Limit: 50})
	if err != nil {
		slog.Error("failed to list links for JSON feed", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}

	items := make([]jsonFeedItem, 0, len(links))
	for _, l := range links {
		contentHTML := l.Commentary
		if contentHTML == "" {
			contentHTML = fmt.Sprintf(`<a href="%s">%s</a>`, template.HTMLEscapeString(l.URL), template.HTMLEscapeString(l.Title))
		}
		items = append(items, jsonFeedItem{
			ID:            fmt.Sprintf("%s/link/%d", s.baseURL, l.ID),
			URL:           fmt.Sprintf("%s/link/%d", s.baseURL, l.ID),
			ExternalURL:   l.URL,
			Title:         l.Title,
			ContentHTML:   contentHTML,
			DatePublished: l.CreatedAt.Format(time.RFC3339),
		})
	}

	feed := jsonFeed{
		Version:     "https://jsonfeed.org/version/1.1",
		Title:       "LinkLog — Joshua Steele",
		HomePageURL: s.baseURL,
		FeedURL:     s.baseURL + "/feed.json",
		Description: "Interesting links with short commentary.",
		Items:       items,
	}

	w.Header().Set("Content-Type", "application/feed+json; charset=utf-8")
	json.NewEncoder(w).Encode(feed)
}

// --- Auth Middleware ---

// requireToken returns middleware that checks for a valid Bearer token.
func (s *Server) requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") || strings.TrimPrefix(auth, "Bearer ") != s.token {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- Helpers ---

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	t, ok := s.templates[name]
	if !ok {
		slog.Error("template not found", "template", name)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "base", data); err != nil {
		slog.Error("template render error", "template", name, "error", err)
	}
}

func pageNum(r *http.Request) int {
	p, err := strconv.Atoi(r.URL.Query().Get("page"))
	if err != nil || p < 1 {
		return 1
	}
	return p
}

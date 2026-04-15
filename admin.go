package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const sessionCookieName = "linklog_session"
const adminSessionDuration = 7 * 24 * time.Hour

// --- Cookie auth middleware ---

// adminRequireAuth redirects to /admin/login if no valid session cookie is present.
func (s *Server) adminRequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil || !s.validAdminSession(cookie.Value) {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// --- Login / logout ---

// adminGetLogin shows the login form.
func (s *Server) adminGetLogin(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil && s.validAdminSession(cookie.Value) {
		http.Redirect(w, r, "/admin", http.StatusSeeOther)
		return
	}
	s.renderAdmin(w, "admin_login.html", map[string]any{"Error": ""})
}

// adminPostLogin handles the login form submission.
func (s *Server) adminPostLogin(w http.ResponseWriter, r *http.Request) {
	if subtle.ConstantTimeCompare([]byte(r.FormValue("password")), []byte(s.adminPass)) != 1 {
		s.renderAdmin(w, "admin_login.html", map[string]any{"Error": "Invalid password."})
		return
	}
	expires := time.Now().Add(adminSessionDuration)
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    s.newAdminSession(expires),
		Path:     "/admin",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(adminSessionDuration.Seconds()),
	})
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// adminPostLogout clears the session cookie.
func (s *Server) adminPostLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/admin",
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})
	http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
}

func (s *Server) newAdminSession(expires time.Time) string {
	exp := strconv.FormatInt(expires.Unix(), 10)
	sig := s.signAdminSession(exp)
	return exp + "." + sig
}

func (s *Server) validAdminSession(value string) bool {
	exp, sig, ok := strings.Cut(value, ".")
	if !ok || exp == "" || sig == "" {
		return false
	}

	expiresUnix, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || time.Now().Unix() > expiresUnix {
		return false
	}

	expected := s.signAdminSession(exp)
	return subtle.ConstantTimeCompare([]byte(sig), []byte(expected)) == 1
}

func (s *Server) signAdminSession(exp string) string {
	mac := hmac.New(sha256.New, []byte(s.adminPass))
	mac.Write([]byte("linklog-admin-session:"))
	mac.Write([]byte(exp))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// --- Admin pages ---

// adminIndex lists all links with edit and delete controls.
func (s *Server) adminIndex(w http.ResponseWriter, r *http.Request) {
	// List all links regardless of published status, no limit for personal use.
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	links, err := s.db.ListLinks(LinkFilter{Query: q, Limit: 500})
	if err != nil {
		slog.Error("admin: failed to list links", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	flash := r.URL.Query().Get("flash")
	s.renderAdmin(w, "admin_index.html", map[string]any{
		"Links": links,
		"Flash": flash,
		"Query": q,
	})
}

// adminGetNew shows the new-link form.
func (s *Server) adminGetNew(w http.ResponseWriter, r *http.Request) {
	s.renderAdmin(w, "admin_new.html", map[string]any{
		"Error": "",
		"Form":  map[string]string{},
	})
}

// adminPostNew handles the new-link form submission.
func (s *Server) adminPostNew(w http.ResponseWriter, r *http.Request) {
	url := strings.TrimSpace(r.FormValue("url"))
	title := strings.TrimSpace(r.FormValue("title"))
	commentary := strings.TrimSpace(r.FormValue("commentary"))
	tags := strings.TrimSpace(r.FormValue("tags"))
	pinned := r.FormValue("pinned") == "on"

	formVals := map[string]string{
		"url": url, "title": title, "commentary": commentary, "tags": tags,
	}
	if pinned {
		formVals["pinned"] = "on"
	}

	if url == "" || (!strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://")) {
		s.renderAdmin(w, "admin_new.html", map[string]any{
			"Error": "URL is required and must start with http:// or https://",
			"Form":  formVals,
		})
		return
	}

	// Fetch title from the page unless the user supplied one.
	meta := FetchPageMeta(url)
	if title != "" {
		meta.Title = title
	}
	if meta.Title == "" {
		meta.Title = url
	}

	link, err := s.db.InsertLink(url, commentary, tags, pinned, meta)
	if err != nil {
		slog.Error("admin: failed to insert link", "error", err)
		s.renderAdmin(w, "admin_new.html", map[string]any{
			"Error": "Failed to save: " + err.Error(),
			"Form":  formVals,
		})
		return
	}

	SendWebmentionAsync(s.db, link.ID, link.URL, fmt.Sprintf("%s/link/%d", s.baseURL, link.ID))

	http.Redirect(w, r, "/admin?flash=Link+created", http.StatusSeeOther)
}

// adminGetEdit shows the edit form for an existing link.
func (s *Server) adminGetEdit(w http.ResponseWriter, r *http.Request) {
	link := s.adminFetchLink(w, r)
	if link == nil {
		return
	}
	s.renderAdmin(w, "admin_edit.html", map[string]any{
		"Link":  link,
		"Error": "",
	})
}

// adminPostEdit handles the edit form submission.
func (s *Server) adminPostEdit(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	url := strings.TrimSpace(r.FormValue("url"))
	title := strings.TrimSpace(r.FormValue("title"))
	commentary := strings.TrimSpace(r.FormValue("commentary"))
	tags := strings.TrimSpace(r.FormValue("tags"))
	description := strings.TrimSpace(r.FormValue("description"))
	siteName := strings.TrimSpace(r.FormValue("site_name"))
	imageURL := strings.TrimSpace(r.FormValue("image_url"))
	canonicalURL := strings.TrimSpace(r.FormValue("canonical_url"))
	published := r.FormValue("published") == "on"
	pinned := r.FormValue("pinned") == "on"

	req := UpdateLinkRequest{
		URL:          &url,
		Title:        &title,
		Commentary:   &commentary,
		Tags:         &tags,
		Description:  &description,
		SiteName:     &siteName,
		ImageURL:     &imageURL,
		CanonicalURL: &canonicalURL,
		Published:    &published,
		Pinned:       &pinned,
	}

	link, err := s.db.UpdateLink(id, req)
	if err != nil {
		slog.Error("admin: failed to update link", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	if link == nil {
		http.NotFound(w, r)
		return
	}

	http.Redirect(w, r, "/admin?flash=Link+saved", http.StatusSeeOther)
}

// adminPostDelete handles link deletion via a POST form.
func (s *Server) adminPostDelete(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if _, err := s.db.DeleteLink(id); err != nil {
		slog.Error("admin: failed to delete link", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/admin?flash=Link+deleted", http.StatusSeeOther)
}

// --- Helpers ---

func (s *Server) adminFetchLink(w http.ResponseWriter, r *http.Request) *Link {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return nil
	}
	link, err := s.db.GetLink(id)
	if err != nil {
		slog.Error("admin: failed to get link", "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return nil
	}
	if link == nil {
		http.NotFound(w, r)
		return nil
	}
	return link
}

// adminPostWebmention re-triggers webmention sending for a link.
func (s *Server) adminPostWebmention(w http.ResponseWriter, r *http.Request) {
	link := s.adminFetchLink(w, r)
	if link == nil {
		return
	}
	// Reset to pending so the UI reflects that a send is in progress.
	if err := s.db.UpdateWebmentionStatus(link.ID, "pending", ""); err != nil {
		slog.Error("admin: failed to reset webmention status", "error", err)
	}
	SendWebmentionAsync(s.db, link.ID, link.URL, fmt.Sprintf("%s/link/%d", s.baseURL, link.ID))
	http.Redirect(w, r, "/admin?flash=Webmention+queued", http.StatusSeeOther)
}

func (s *Server) renderAdmin(w http.ResponseWriter, name string, data any) {
	t, ok := s.templates[name]
	if !ok {
		slog.Error("admin template not found", "template", name)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := t.ExecuteTemplate(w, "admin_base", data); err != nil {
		slog.Error("admin template render error", "template", name, "error", err)
	}
}

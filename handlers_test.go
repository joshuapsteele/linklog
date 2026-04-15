package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFeedPinnedJSONOnlyReturnsPublishedPinnedLinks(t *testing.T) {
	db := openTestDB(t)

	pinned := insertTestLink(t, db, "https://example.com/pinned", "Pinned Link", "", "", true, PageMeta{
		Title:       "Pinned Link",
		Description: "Pinned description",
		ImageURL:    "https://example.com/pinned.png",
	})
	insertTestLink(t, db, "https://example.com/plain", "Plain Link", "", "", false, PageMeta{
		Title: "Plain Link",
	})
	unpublished := insertTestLink(t, db, "https://example.com/draft", "Draft Link", "", "", true, PageMeta{
		Title: "Draft Link",
	})
	published := false
	if _, err := db.UpdateLink(unpublished.ID, UpdateLinkRequest{Published: &published}); err != nil {
		t.Fatalf("mark pinned link unpublished: %v", err)
	}

	srv := &Server{db: db, baseURL: "https://links.example.test"}
	req := httptest.NewRequest(http.MethodGet, "/pinned/feed.json", nil)
	rec := httptest.NewRecorder()

	srv.feedPinnedJSON(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("expected CORS header *, got %q", got)
	}

	var feed jsonFeed
	if err := json.NewDecoder(rec.Body).Decode(&feed); err != nil {
		t.Fatalf("decode feed: %v", err)
	}
	if feed.Title != "Pinned Links — Joshua Steele" {
		t.Fatalf("unexpected feed title %q", feed.Title)
	}
	if feed.FeedURL != "https://links.example.test/pinned/feed.json" {
		t.Fatalf("unexpected feed URL %q", feed.FeedURL)
	}
	if len(feed.Items) != 1 {
		t.Fatalf("expected 1 item, got %d: %+v", len(feed.Items), feed.Items)
	}
	if feed.Items[0].ID != "https://links.example.test/link/1" || feed.Items[0].ExternalURL != pinned.URL || feed.Items[0].Summary != "Pinned description" {
		t.Fatalf("unexpected pinned feed item: %+v", feed.Items[0])
	}
	if feed.Items[0].Image != "https://example.com/pinned.png" {
		t.Fatalf("expected metadata image in feed item, got %q", feed.Items[0].Image)
	}
}

func TestAPICreateLinkReturnsExistingLinkForDuplicateURL(t *testing.T) {
	db := openTestDB(t)
	existing := insertTestLink(t, db, "https://example.com/duplicate", "Original Link", "original note", "original", false, PageMeta{
		Title: "Original Link",
	})

	srv := &Server{db: db, baseURL: "https://links.example.test"}
	body := bytes.NewBufferString(`{"url":"https://example.com/duplicate","commentary":"new note","tags":"new","pinned":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/links", body)
	rec := httptest.NewRecorder()

	srv.apiCreateLink(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-LinkLog-Duplicate"); got != "true" {
		t.Fatalf("expected duplicate header true, got %q", got)
	}
	if got := rec.Header().Get("Location"); got != "https://links.example.test/link/1" {
		t.Fatalf("unexpected Location header %q", got)
	}

	var link Link
	if err := json.NewDecoder(rec.Body).Decode(&link); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if link.ID != existing.ID || link.Commentary != "original note" || link.Tags != "original" || link.Pinned {
		t.Fatalf("expected existing unmodified link, got %+v", link)
	}

	links, err := db.ListLinks(LinkFilter{})
	if err != nil {
		t.Fatalf("list links: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected duplicate request not to create a row, got %d rows", len(links))
	}
}

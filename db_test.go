package main

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestOpenDBCreatesExpectedSchema(t *testing.T) {
	db := openTestDB(t)

	columns := tableColumns(t, db)
	for _, column := range []string{
		"id",
		"url",
		"title",
		"commentary",
		"tags",
		"description",
		"site_name",
		"image_url",
		"canonical_url",
		"published",
		"pinned",
		"webmention_status",
		"webmention_endpoint",
	} {
		if !columns[column] {
			t.Fatalf("expected links.%s column to exist", column)
		}
	}

	indexes := tableIndexes(t, db)
	for _, index := range []string{"idx_links_created_at", "idx_links_published", "idx_links_pinned"} {
		if !indexes[index] {
			t.Fatalf("expected %s index to exist", index)
		}
	}
}

func TestOpenDBMigratesOldSchemaAndScansExistingRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open old db: %v", err)
	}
	_, err = old.Exec(`
		CREATE TABLE links (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			url TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			commentary TEXT NOT NULL DEFAULT '',
			tags TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			published BOOLEAN NOT NULL DEFAULT 1
		);
		INSERT INTO links (url, title, commentary, tags)
		VALUES ('https://example.com', 'Example Domain', 'Old row', 'Go, Indie Web, go');
	`)
	if err != nil {
		t.Fatalf("create old schema: %v", err)
	}
	if err := old.Close(); err != nil {
		t.Fatalf("close old db: %v", err)
	}

	db, err := OpenDB(path)
	if err != nil {
		t.Fatalf("open migrated db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	columns := tableColumns(t, db)
	for _, column := range []string{
		"description",
		"site_name",
		"image_url",
		"canonical_url",
		"pinned",
		"webmention_status",
		"webmention_endpoint",
	} {
		if !columns[column] {
			t.Fatalf("expected migrated links.%s column to exist", column)
		}
	}

	link, err := db.GetLink(1)
	if err != nil {
		t.Fatalf("get migrated link: %v", err)
	}
	if link == nil {
		t.Fatal("expected migrated link")
	}
	if link.Title != "Example Domain" || link.Description != "" || link.Pinned || link.Tags != "go,indie-web" {
		t.Fatalf("unexpected migrated link: %+v", link)
	}
	if link.WebmentionStatus != "pending" {
		t.Fatalf("expected default webmention status pending, got %q", link.WebmentionStatus)
	}
}

func TestListLinksPinnedFilter(t *testing.T) {
	db := openTestDB(t)

	insertTestLink(t, db, "https://example.com/pinned", "Pinned", "note", "test", true, PageMeta{Title: "Pinned"})
	insertTestLink(t, db, "https://example.com/plain", "Plain", "note", "test", false, PageMeta{Title: "Plain"})

	pinned := true
	links, err := db.ListLinks(LinkFilter{Pinned: &pinned})
	if err != nil {
		t.Fatalf("list pinned links: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("expected 1 pinned link, got %d: %+v", len(links), links)
	}
	if links[0].Title != "Pinned" || !links[0].Pinned {
		t.Fatalf("unexpected pinned link: %+v", links[0])
	}
}

func TestNormalizeTags(t *testing.T) {
	got := NormalizeTags(" Go, #Indie Web,go, tools!,  ")
	want := "go,indie-web,tools"
	if got != want {
		t.Fatalf("NormalizeTags() = %q, want %q", got, want)
	}
}

func TestInsertAndUpdateNormalizeTags(t *testing.T) {
	db := openTestDB(t)

	link := insertTestLink(t, db, "https://example.com/tags", "Tags", "note", "Go, Indie Web, go", false, PageMeta{Title: "Tags"})
	if link.Tags != "go,indie-web" {
		t.Fatalf("expected inserted tags to be normalized, got %q", link.Tags)
	}

	tags := "SQLite, Personal Knowledge Management, sqlite"
	updated, err := db.UpdateLink(link.ID, UpdateLinkRequest{Tags: &tags})
	if err != nil {
		t.Fatalf("update tags: %v", err)
	}
	if updated.Tags != "sqlite,personal-knowledge-management" {
		t.Fatalf("expected updated tags to be normalized, got %q", updated.Tags)
	}
}

func TestListLinksTagFilterUsesNormalizedTags(t *testing.T) {
	db := openTestDB(t)

	insertTestLink(t, db, "https://example.com/one", "One", "note", "Indie Web", false, PageMeta{Title: "One"})
	insertTestLink(t, db, "https://example.com/two", "Two", "note", "go", false, PageMeta{Title: "Two"})

	links, err := db.ListLinks(LinkFilter{Tag: "indie web"})
	if err != nil {
		t.Fatalf("list normalized tag: %v", err)
	}
	if len(links) != 1 || links[0].Title != "One" {
		t.Fatalf("unexpected normalized tag results: %+v", links)
	}
}

func TestListTagCounts(t *testing.T) {
	db := openTestDB(t)

	insertTestLink(t, db, "https://example.com/one", "One", "note", "go,tools", false, PageMeta{Title: "One"})
	insertTestLink(t, db, "https://example.com/two", "Two", "note", "go", false, PageMeta{Title: "Two"})
	draft := insertTestLink(t, db, "https://example.com/draft", "Draft", "note", "draft,go", false, PageMeta{Title: "Draft"})
	published := false
	if _, err := db.UpdateLink(draft.ID, UpdateLinkRequest{Published: &published}); err != nil {
		t.Fatalf("mark draft unpublished: %v", err)
	}

	tags, err := db.ListTagCounts()
	if err != nil {
		t.Fatalf("list tag counts: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 published tags, got %+v", tags)
	}
	if tags[0] != (TagCount{Name: "go", Count: 2}) {
		t.Fatalf("unexpected first tag count: %+v", tags[0])
	}
	if tags[1] != (TagCount{Name: "tools", Count: 1}) {
		t.Fatalf("unexpected second tag count: %+v", tags[1])
	}
}

func TestGetLinkByURL(t *testing.T) {
	db := openTestDB(t)

	inserted := insertTestLink(t, db, "https://example.com/saved", "Saved", "note", "test", false, PageMeta{Title: "Saved"})

	found, err := db.GetLinkByURL("https://example.com/saved")
	if err != nil {
		t.Fatalf("get link by URL: %v", err)
	}
	if found == nil {
		t.Fatal("expected link to be found")
	}
	if found.ID != inserted.ID || found.Title != "Saved" {
		t.Fatalf("unexpected found link: %+v", found)
	}

	missing, err := db.GetLinkByURL("https://example.com/missing")
	if err != nil {
		t.Fatalf("get missing link by URL: %v", err)
	}
	if missing != nil {
		t.Fatalf("expected missing URL to return nil, got %+v", missing)
	}
}

func TestListLinksSearchesMetadataFields(t *testing.T) {
	db := openTestDB(t)

	insertTestLink(t, db, "https://example.com/one", "Ordinary", "plain note", "misc", false, PageMeta{
		Title:        "Ordinary",
		Description:  "description needle",
		SiteName:     "site needle",
		CanonicalURL: "https://example.com/canonical-needle",
	})

	for _, query := range []string{"description needle", "site needle", "canonical-needle"} {
		links, err := db.ListLinks(LinkFilter{Query: query})
		if err != nil {
			t.Fatalf("search %q: %v", query, err)
		}
		if len(links) != 1 {
			t.Fatalf("expected 1 result for %q, got %d: %+v", query, len(links), links)
		}
		if links[0].Title != "Ordinary" {
			t.Fatalf("unexpected result for %q: %+v", query, links[0])
		}
	}
}

func TestUpdateLinkMetadataPreservesUserFields(t *testing.T) {
	db := openTestDB(t)

	link := insertTestLink(t, db, "https://example.com/keep", "Keep Title", "Keep commentary", "keep,tags", true, PageMeta{
		Title:       "Keep Title",
		Description: "Old description",
		SiteName:    "Old Site",
		ImageURL:    "https://example.com/old.png",
	})
	published := false
	if _, err := db.UpdateLink(link.ID, UpdateLinkRequest{Published: &published}); err != nil {
		t.Fatalf("mark unpublished: %v", err)
	}

	updated, err := db.UpdateLinkMetadata(link.ID, PageMeta{
		Title:        "Fetched Title",
		Description:  "New description",
		SiteName:     "New Site",
		ImageURL:     "https://example.com/new.png",
		CanonicalURL: "https://example.com/canonical",
	}, false)
	if err != nil {
		t.Fatalf("update metadata: %v", err)
	}

	if updated.Title != "Keep Title" {
		t.Fatalf("expected title to be preserved, got %q", updated.Title)
	}
	if updated.Commentary != "Keep commentary" || updated.Tags != "keep,tags" || !updated.Pinned || updated.Published {
		t.Fatalf("user fields were not preserved: %+v", updated)
	}
	if updated.Description != "New description" || updated.SiteName != "New Site" || updated.ImageURL != "https://example.com/new.png" || updated.CanonicalURL != "https://example.com/canonical" {
		t.Fatalf("metadata was not updated: %+v", updated)
	}
}

func TestUpdateLinkMetadataCanOverwriteTitle(t *testing.T) {
	db := openTestDB(t)

	link := insertTestLink(t, db, "https://example.com/title", "https://example.com/title", "", "", false, PageMeta{
		Title: "https://example.com/title",
	})

	updated, err := db.UpdateLinkMetadata(link.ID, PageMeta{Title: "Fetched Title"}, true)
	if err != nil {
		t.Fatalf("update metadata: %v", err)
	}
	if updated.Title != "Fetched Title" {
		t.Fatalf("expected title overwrite, got %q", updated.Title)
	}
}

func openTestDB(t *testing.T) *DB {
	t.Helper()

	db, err := OpenDB(filepath.Join(t.TempDir(), "linklog.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func insertTestLink(t *testing.T, db *DB, url, title, commentary, tags string, pinned bool, meta PageMeta) *Link {
	t.Helper()

	if meta.Title == "" {
		meta.Title = title
	}
	link, err := db.InsertLink(url, commentary, tags, pinned, meta)
	if err != nil {
		t.Fatalf("insert link %q: %v", title, err)
	}
	return link
}

func tableColumns(t *testing.T, db *DB) map[string]bool {
	t.Helper()

	rows, err := db.conn.Query(`PRAGMA table_info(links)`)
	if err != nil {
		t.Fatalf("table info: %v", err)
	}
	defer rows.Close()

	columns := make(map[string]bool)
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var defaultValue sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan table info: %v", err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("table info rows: %v", err)
	}
	return columns
}

func tableIndexes(t *testing.T, db *DB) map[string]bool {
	t.Helper()

	rows, err := db.conn.Query(`PRAGMA index_list(links)`)
	if err != nil {
		t.Fatalf("index list: %v", err)
	}
	defer rows.Close()

	indexes := make(map[string]bool)
	for rows.Next() {
		var seq int
		var name string
		var unique int
		var origin string
		var partial int
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			t.Fatalf("scan index list: %v", err)
		}
		indexes[name] = true
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("index list rows: %v", err)
	}
	return indexes
}

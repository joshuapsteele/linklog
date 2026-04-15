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
		VALUES ('https://example.com', 'Example Domain', 'Old row', 'old');
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
	if link.Title != "Example Domain" || link.Description != "" || link.Pinned {
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

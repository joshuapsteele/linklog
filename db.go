package main

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps a sql.DB connection to the SQLite database.
type DB struct {
	conn *sql.DB
}

// OpenDB opens the SQLite database at the given path and runs migrations.
func OpenDB(path string) (*DB, error) {
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// SQLite pragmas for performance and safety.
	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA busy_timeout=5000",
		"PRAGMA foreign_keys=ON",
	}
	for _, p := range pragmas {
		if _, err := conn.Exec(p); err != nil {
			return nil, fmt.Errorf("exec pragma %q: %w", p, err)
		}
	}

	db := &DB{conn: conn}
	if err := db.migrate(); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return db, nil
}

// Close closes the database connection.
func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS links (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		url TEXT NOT NULL,
		title TEXT NOT NULL DEFAULT '',
		commentary TEXT NOT NULL DEFAULT '',
		tags TEXT NOT NULL DEFAULT '',
		description TEXT NOT NULL DEFAULT '',
		site_name TEXT NOT NULL DEFAULT '',
		image_url TEXT NOT NULL DEFAULT '',
		canonical_url TEXT NOT NULL DEFAULT '',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		published BOOLEAN NOT NULL DEFAULT 1,
		pinned BOOLEAN NOT NULL DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_links_created_at ON links(created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_links_published ON links(published);
	`
	if _, err := db.conn.Exec(schema); err != nil {
		return err
	}

	// Additive migrations: ALTER TABLE does not support IF NOT EXISTS in SQLite,
	// so we attempt each and silently ignore "duplicate column" errors.
	db.conn.Exec(`ALTER TABLE links ADD COLUMN webmention_status TEXT NOT NULL DEFAULT 'pending'`)
	db.conn.Exec(`ALTER TABLE links ADD COLUMN webmention_endpoint TEXT NOT NULL DEFAULT ''`)
	db.conn.Exec(`ALTER TABLE links ADD COLUMN pinned BOOLEAN NOT NULL DEFAULT 0`)
	db.conn.Exec(`ALTER TABLE links ADD COLUMN description TEXT NOT NULL DEFAULT ''`)
	db.conn.Exec(`ALTER TABLE links ADD COLUMN site_name TEXT NOT NULL DEFAULT ''`)
	db.conn.Exec(`ALTER TABLE links ADD COLUMN image_url TEXT NOT NULL DEFAULT ''`)
	db.conn.Exec(`ALTER TABLE links ADD COLUMN canonical_url TEXT NOT NULL DEFAULT ''`)

	if _, err := db.conn.Exec(`CREATE INDEX IF NOT EXISTS idx_links_pinned ON links(pinned)`); err != nil {
		return err
	}

	return nil
}

// InsertLink inserts a new link and returns it with its assigned ID and timestamps.
func (db *DB) InsertLink(url, commentary, tags string, pinned bool, meta PageMeta) (*Link, error) {
	now := time.Now().UTC()
	result, err := db.conn.Exec(
		`INSERT INTO links (url, title, commentary, tags, description, site_name, image_url, canonical_url, created_at, updated_at, published, pinned, webmention_status, webmention_endpoint)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, 'pending', '')`,
		url, meta.Title, commentary, tags, meta.Description, meta.SiteName, meta.ImageURL, meta.CanonicalURL, now, now, pinned,
	)
	if err != nil {
		return nil, fmt.Errorf("insert link: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("last insert id: %w", err)
	}

	return &Link{
		ID:               id,
		URL:              url,
		Title:            meta.Title,
		Commentary:       commentary,
		Tags:             tags,
		Description:      meta.Description,
		SiteName:         meta.SiteName,
		ImageURL:         meta.ImageURL,
		CanonicalURL:     meta.CanonicalURL,
		CreatedAt:        now,
		UpdatedAt:        now,
		Published:        true,
		Pinned:           pinned,
		WebmentionStatus: "pending",
	}, nil
}

// UpdateWebmentionStatus sets the webmention_status and webmention_endpoint for a link.
func (db *DB) UpdateWebmentionStatus(id int64, status, endpoint string) error {
	_, err := db.conn.Exec(
		`UPDATE links SET webmention_status = ?, webmention_endpoint = ? WHERE id = ?`,
		status, endpoint, id,
	)
	return err
}

// GetLink returns a single link by ID, or nil if not found.
func (db *DB) GetLink(id int64) (*Link, error) {
	row := db.conn.QueryRow(
		`SELECT id, url, title, commentary, tags, description, site_name, image_url, canonical_url, created_at, updated_at, published, pinned, webmention_status, webmention_endpoint
		 FROM links WHERE id = ?`, id,
	)
	return scanLink(row)
}

// DeleteLink deletes a link by ID. Returns true if a row was deleted.
func (db *DB) DeleteLink(id int64) (bool, error) {
	result, err := db.conn.Exec(`DELETE FROM links WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("delete link: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return n > 0, nil
}

// UpdateLink applies partial updates to a link. Returns the updated link or nil if not found.
func (db *DB) UpdateLink(id int64, req UpdateLinkRequest) (*Link, error) {
	sets := make([]string, 0)
	args := make([]any, 0)

	if req.URL != nil {
		sets = append(sets, "url = ?")
		args = append(args, *req.URL)
	}
	if req.Title != nil {
		sets = append(sets, "title = ?")
		args = append(args, *req.Title)
	}
	if req.Commentary != nil {
		sets = append(sets, "commentary = ?")
		args = append(args, *req.Commentary)
	}
	if req.Tags != nil {
		sets = append(sets, "tags = ?")
		args = append(args, *req.Tags)
	}
	if req.Description != nil {
		sets = append(sets, "description = ?")
		args = append(args, *req.Description)
	}
	if req.SiteName != nil {
		sets = append(sets, "site_name = ?")
		args = append(args, *req.SiteName)
	}
	if req.ImageURL != nil {
		sets = append(sets, "image_url = ?")
		args = append(args, *req.ImageURL)
	}
	if req.CanonicalURL != nil {
		sets = append(sets, "canonical_url = ?")
		args = append(args, *req.CanonicalURL)
	}
	if req.Published != nil {
		sets = append(sets, "published = ?")
		args = append(args, *req.Published)
	}
	if req.Pinned != nil {
		sets = append(sets, "pinned = ?")
		args = append(args, *req.Pinned)
	}

	if len(sets) == 0 {
		return db.GetLink(id)
	}

	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now().UTC())
	args = append(args, id)

	query := fmt.Sprintf("UPDATE links SET %s WHERE id = ?", strings.Join(sets, ", "))
	result, err := db.conn.Exec(query, args...)
	if err != nil {
		return nil, fmt.Errorf("update link: %w", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return nil, nil
	}

	return db.GetLink(id)
}

// LinkFilter holds optional query parameters for listing links.
type LinkFilter struct {
	Tag       string
	Query     string
	Published *bool
	Pinned    *bool
	Limit     int
	Offset    int
}

// ListLinks returns links matching the given filter, ordered by created_at DESC.
func (db *DB) ListLinks(f LinkFilter) ([]Link, error) {
	where := make([]string, 0)
	args := make([]any, 0)

	if f.Tag != "" {
		// Match tag as a whole word within comma-separated list.
		where = append(where, "(tags = ? OR tags LIKE ? OR tags LIKE ? OR tags LIKE ?)")
		args = append(args, f.Tag, f.Tag+",%", "%,"+f.Tag, "%,"+f.Tag+",%")
	}
	if f.Query != "" {
		where = append(where, "(title LIKE ? OR url LIKE ? OR commentary LIKE ? OR tags LIKE ? OR description LIKE ? OR site_name LIKE ? OR canonical_url LIKE ?)")
		like := "%" + f.Query + "%"
		args = append(args, like, like, like, like, like, like, like)
	}
	if f.Published != nil {
		where = append(where, "published = ?")
		args = append(args, *f.Published)
	}
	if f.Pinned != nil {
		where = append(where, "pinned = ?")
		args = append(args, *f.Pinned)
	}

	query := "SELECT id, url, title, commentary, tags, description, site_name, image_url, canonical_url, created_at, updated_at, published, pinned, webmention_status, webmention_endpoint FROM links"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY created_at DESC"

	if f.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", f.Limit)
	}
	if f.Offset > 0 {
		query += fmt.Sprintf(" OFFSET %d", f.Offset)
	}

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list links: %w", err)
	}
	defer rows.Close()

	var links []Link
	for rows.Next() {
		l, err := scanLinkRow(rows)
		if err != nil {
			return nil, err
		}
		links = append(links, *l)
	}
	return links, rows.Err()
}

// CountLinks returns the number of links matching the given filter (for pagination).
func (db *DB) CountLinks(f LinkFilter) (int, error) {
	where := make([]string, 0)
	args := make([]any, 0)

	if f.Tag != "" {
		where = append(where, "(tags = ? OR tags LIKE ? OR tags LIKE ? OR tags LIKE ?)")
		args = append(args, f.Tag, f.Tag+",%", "%,"+f.Tag, "%,"+f.Tag+",%")
	}
	if f.Query != "" {
		where = append(where, "(title LIKE ? OR url LIKE ? OR commentary LIKE ? OR tags LIKE ? OR description LIKE ? OR site_name LIKE ? OR canonical_url LIKE ?)")
		like := "%" + f.Query + "%"
		args = append(args, like, like, like, like, like, like, like)
	}
	if f.Published != nil {
		where = append(where, "published = ?")
		args = append(args, *f.Published)
	}
	if f.Pinned != nil {
		where = append(where, "pinned = ?")
		args = append(args, *f.Pinned)
	}

	query := "SELECT COUNT(*) FROM links"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	var count int
	err := db.conn.QueryRow(query, args...).Scan(&count)
	return count, err
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

func scanLink(s scanner) (*Link, error) {
	var l Link
	var createdAt, updatedAt string
	err := s.Scan(
		&l.ID, &l.URL, &l.Title, &l.Commentary, &l.Tags,
		&l.Description, &l.SiteName, &l.ImageURL, &l.CanonicalURL,
		&createdAt, &updatedAt, &l.Published, &l.Pinned,
		&l.WebmentionStatus, &l.WebmentionEndpoint,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan link: %w", err)
	}

	l.CreatedAt, _ = time.Parse("2006-01-02 15:04:05", createdAt)
	if l.CreatedAt.IsZero() {
		l.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	}
	l.UpdatedAt, _ = time.Parse("2006-01-02 15:04:05", updatedAt)
	if l.UpdatedAt.IsZero() {
		l.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	}

	return &l, nil
}

func scanLinkRow(rows *sql.Rows) (*Link, error) {
	return scanLink(rows)
}

// boolPtr is a helper for creating *bool values.
func boolPtr(b bool) *bool {
	return &b
}

// logDBError logs a database error with context.
func logDBError(operation string, err error) {
	slog.Error("database error", "operation", operation, "error", err)
}

# LinkLog: Project Specification

## What This Is

LinkLog is a personal link blog. It's a small Go web application that lets me save interesting URLs with short commentary and publishes them at `links.joshuapsteele.com`. The primary input path is a Drafts action on iOS. The output is a public webpage with an RSS/Atom feed.

This is a learning project. The goals (in order of priority) are:

1. Learn Go by building something real.
2. Learn to deploy and manage a server (VPS, systemd, Caddy, DNS).
3. End up with a tool I'll actually use as part of my online presence.

The application is intentionally simple. One user (me), one server, one SQLite database file. No frameworks, no containers, no CI/CD pipeline. Compile a binary, copy it to the server, restart the service.


## Architecture Overview

A Go binary runs on a DigitalOcean droplet ($6/month, 1 vCPU, 1GB RAM, Ubuntu). Caddy sits in front as a reverse proxy and handles HTTPS via automatic Let's Encrypt certificates. The Go app listens on localhost:8080. Data lives in a single SQLite file on disk.

```
[Drafts on iPhone]
        |
        | POST /api/links (Bearer token)
        v
[Caddy on VPS] --> [Go binary :8080] --> [SQLite file]
        |
        | serves HTML, RSS
        v
[Browser / RSS reader / Micro.blog]
```


## Data Model

One primary table. Keep it flat.

```sql
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

CREATE INDEX idx_links_created_at ON links(created_at DESC);
CREATE INDEX idx_links_published ON links(published);
```

Notes on the schema:

- `tags` is a comma-separated string (e.g., "pkm,indieweb,go"). This is simpler than a join table and fine for the scale of this project. Normalize later if needed.
- `published` defaults to `1` (true). Everything is published immediately unless I explicitly mark it as a draft. The Drafts action workflow means I've already decided to publish by the time I hit the button.
- `title` is populated by the server when it fetches the URL. If the fetch fails, it falls back to the raw URL.


## API Endpoints

All API endpoints are authenticated with a Bearer token. The token is a long random string stored as an environment variable (`LINKLOG_API_TOKEN`). The app checks the `Authorization: Bearer <token>` header on every API request.

### POST /api/links

Create a new link entry.

Request body (JSON):
```json
{
  "url": "https://example.com/interesting-article",
  "commentary": "This is a sharp take on the state of web development.",
  "tags": "webdev,indieweb"
}
```

Behavior:

1. Validate that `url` is present and looks like a URL.
2. Fetch the page at the URL and extract the `<title>` tag. If the fetch fails or times out (5-second timeout), use the raw URL as the title.
3. Also attempt to extract the `og:description` or `meta description` tag and store it (add a `description` column if desired, or just discard it; the commentary is the point).
4. Insert the record into SQLite.
5. Return 201 with the created link as JSON.

Response (JSON):
```json
{
  "id": 42,
  "url": "https://example.com/interesting-article",
  "title": "The State of Web Development in 2026",
  "commentary": "This is a sharp take on the state of web development.",
  "tags": "webdev,indieweb",
  "created_at": "2026-04-13T14:30:00Z",
  "published": true
}
```

### DELETE /api/links/{id}

Delete a link entry by ID. Returns 204 on success, 404 if not found.

### PATCH /api/links/{id}

Update a link entry. Accepts partial JSON (only the fields being changed). Returns the updated link as JSON.

### GET /api/links

List all links as JSON (authenticated). Useful for debugging or future integrations. Supports optional query parameters: `?tag=pkm`, `?published=false`, `?limit=50`.


## Public Pages

These are unauthenticated. Served as HTML from Go templates.

### GET /

The main link feed. Shows the most recent links in reverse chronological order, paginated (20 per page). Each entry displays the title (linked to the original URL), my commentary, tags (each linked to a tag filter page), and a relative timestamp ("3 hours ago" / "yesterday" / "April 10, 2026"). Include a permalink to the entry on my site (e.g., `links.joshuapsteele.com/link/42`).

Pagination via `?page=2`, `?page=3`, etc. Show "Older" and "Newer" links at the bottom.

### GET /link/{id}

Permalink page for a single link entry. Same display as the feed but for one item. Useful for sharing a specific link post.

### GET /tag/{tag}

Filtered feed showing only links with the given tag. Same layout as the main feed.

### GET /feed.xml

RSS 2.0 feed of the most recent 50 published links. Include the commentary as the `<description>` and the original URL as the `<link>`. The feed URL is what Micro.blog will subscribe to, so it should be well-formed and stable.

### GET /feed.json

JSON Feed (https://www.jsonfeed.org/) of the most recent 50 published links. Optional but easy to implement and some feed readers prefer it.


## HTML Templates and Styling

Use Go's `html/template` package. Keep the template structure simple:

- `base.html`: The outer shell (doctype, head, body wrapper, footer). Defines blocks that child templates fill in.
- `feed.html`: The main link list (extends base).
- `single.html`: A single link entry page (extends base).
- `tag.html`: Tag-filtered feed (extends base, reuses feed layout).

Styling should feel like it belongs alongside joshuapsteele.com. Don't use a CSS framework. Write a small CSS file by hand. Priorities:

- Clean, readable typography. Use a system font stack or match the fonts on joshuapsteele.com.
- Generous whitespace. Each link entry should breathe.
- Responsive without being fancy. It should look fine on a phone.
- Minimal decoration. No hero images, no sidebar, no widgets.
- A small header with "LinkLog" (or similar) and a link back to joshuapsteele.com.
- A footer with links to the RSS feed and maybe joshuapsteele.com, social.joshuapsteele.com.

Before building the templates, fetch the current joshuapsteele.com homepage and note the fonts, colors, and general feel so the link blog doesn't look like a completely different site.


## Drafts Action (iOS Input)

The Drafts action is the primary way I'll create link posts. The action should work as follows:

1. I open Drafts on my phone and create a new draft.
2. First line: the URL.
3. Second line (optional): comma-separated tags.
4. Remaining lines: my commentary.

Example draft content:
```
https://stevehanov.ca/blog/how-i-run-multiple-10k-mrr-companies-on-a-20month-tech-stack
go,indieweb,lean
This guy runs several profitable products on a $5 VPS. The whole post is a good argument for keeping things simple.
```

The Drafts action is a JavaScript step that:

1. Parses the draft content (first line = URL, second line = tags, rest = commentary).
2. Sends a POST request to `https://links.joshuapsteele.com/api/links` with the JSON body and the Bearer token in the Authorization header.
3. If successful, shows a success banner and optionally archives the draft.
4. If it fails, shows the error and keeps the draft active.

The Drafts action script (JavaScript):

```javascript
const lines = draft.content.split("\n");
const url = lines[0].trim();
const tags = lines.length > 1 ? lines[1].trim() : "";
const commentary = lines.length > 2 ? lines.slice(2).join("\n").trim() : "";

const apiUrl = "https://links.joshuapsteele.com/api/links";
const token = "YOUR_API_TOKEN_HERE"; // Replace with credential lookup

var http = HTTP.create();
var response = http.request({
  url: apiUrl,
  method: "POST",
  headers: {
    "Content-Type": "application/json",
    "Authorization": "Bearer " + token
  },
  data: {
    url: url,
    commentary: commentary,
    tags: tags
  }
});

if (response.success) {
  app.displaySuccessMessage("Link posted!");
  draft.isFlagged = false;
  draft.update();
  // Optionally: draft.isArchived = true; draft.update();
} else {
  app.displayErrorMessage("Failed: " + response.statusCode);
  context.fail();
}
```

Store the API token as a Drafts credential rather than hardcoding it. Drafts supports a `Credential` object for this. Update the script to use `Credential.create("LinkLog", "API Token")` to prompt for the token on first use and store it securely.


## URL Fetching

When a link is created, the server fetches the URL to extract metadata. This should be resilient:

- Use a 5-second timeout on the HTTP request.
- Set a reasonable User-Agent header (e.g., "LinkLog/1.0 (+https://links.joshuapsteele.com)").
- Parse the HTML response to extract the `<title>` tag content.
- Optionally extract `og:title` (prefer over `<title>` if present), `og:description`, and `og:image`.
- If the fetch fails for any reason (timeout, non-HTML response, DNS failure, etc.), use the raw URL as the title and proceed. Never fail the link creation because the fetch failed.
- Don't follow more than 3 redirects.

Use Go's `net/http` for the request and `golang.org/x/net/html` for parsing. Don't pull in a full scraping library.


## Project Structure

```
linklog/
├── main.go              # Entry point, server startup, config loading
├── handlers.go          # HTTP handler functions (API + public pages)
├── db.go                # SQLite initialization and query functions
├── fetch.go             # URL fetching and metadata extraction
├── models.go            # Link struct and any helpers
├── templates/
│   ├── base.html
│   ├── feed.html
│   ├── single.html
│   └── tag.html
├── static/
│   └── style.css
├── drafts/
│   └── linklog-action.js   # Drafts action for reference
├── go.mod
├── go.sum
├── README.md
└── Makefile             # Build and deploy shortcuts
```


## Dependencies

Keep these minimal:

- `github.com/go-chi/chi/v5` for routing. Lightweight, idiomatic, makes path parameters clean.
- `modernc.org/sqlite` for SQLite. Pure Go implementation, no CGo required, cross-compiles easily.
- `golang.org/x/net/html` for HTML parsing (URL metadata extraction).

That's it. No ORM, no CSS framework, no template engine beyond the standard library, no logging library beyond `log/slog` (included in Go's standard library since Go 1.21).


## Configuration

All configuration via environment variables. No config file.

- `LINKLOG_API_TOKEN` (required): The Bearer token for API authentication.
- `LINKLOG_DB_PATH` (optional, default: `./linklog.db`): Path to the SQLite database file.
- `LINKLOG_PORT` (optional, default: `8080`): Port to listen on.
- `LINKLOG_BASE_URL` (optional, default: `https://links.joshuapsteele.com`): Used for generating permalinks and feed URLs.

On the server, these are set in the systemd service file.


## Deployment

### Server Setup (One-Time)

1. Provision a DigitalOcean droplet: $6/month, 1 vCPU, 1GB RAM, 25GB SSD, Ubuntu 24.04, US East (NYC) region.
2. SSH in. Create a non-root user. Set up SSH key auth. Disable password login.
3. Install Caddy (see https://caddyserver.com/docs/install for the apt repo method).
4. Configure Caddy (`/etc/caddy/Caddyfile`):

```
links.joshuapsteele.com {
    reverse_proxy localhost:8080
}
```

Caddy automatically provisions and renews HTTPS certificates from Let's Encrypt. No further TLS configuration needed.

5. Point DNS: Add an A record for `links.joshuapsteele.com` pointing to the droplet's IP address. Do this in whatever DNS provider manages joshuapsteele.com (likely Netlify DNS or the domain registrar).

6. Create the application directory and systemd service:

```bash
sudo mkdir -p /opt/linklog
sudo chown $USER:$USER /opt/linklog
```

Systemd service file (`/etc/systemd/system/linklog.service`):

```ini
[Unit]
Description=LinkLog
After=network.target

[Service]
Type=simple
User=linklog
WorkingDirectory=/opt/linklog
ExecStart=/opt/linklog/linklog
Restart=on-failure
RestartSec=5
Environment=LINKLOG_API_TOKEN=your-long-random-token-here
Environment=LINKLOG_DB_PATH=/opt/linklog/linklog.db
Environment=LINKLOG_BASE_URL=https://links.joshuapsteele.com

[Install]
WantedBy=multi-user.target
```

### Deploy Process (Repeatable)

Build on my Mac and copy to the server. This can be a Makefile target or a small shell script.

```makefile
deploy:
	GOOS=linux GOARCH=amd64 go build -o linklog .
	scp linklog linklog-server:/opt/linklog/linklog
	ssh linklog-server 'sudo systemctl restart linklog'
```

Where `linklog-server` is an SSH config alias for the droplet.

The templates and static files should be embedded in the binary using Go's `embed` package so that the single binary is the entire application with no extra files to copy. This is one of Go's best features for this kind of deployment.

### Backups

A daily cron job copies the SQLite file to a backup location. SQLite is safe to copy while the WAL is enabled as long as you copy the `-wal` and `-shm` files too, or use the `.backup` command via the sqlite3 CLI.

```bash
# /etc/cron.d/linklog-backup
0 3 * * * linklog sqlite3 /opt/linklog/linklog.db ".backup /opt/linklog/backups/linklog-$(date +\%Y\%m\%d).db"
```

Keep the last 30 days of backups. Add a cleanup line or a simple rotation script.


## SQLite Configuration

On database initialization, run these pragmas:

```sql
PRAGMA journal_mode=WAL;
PRAGMA synchronous=NORMAL;
PRAGMA busy_timeout=5000;
PRAGMA foreign_keys=ON;
```

WAL mode allows concurrent reads while writing. `busy_timeout` tells SQLite to wait up to 5 seconds if the database is locked rather than returning an error immediately. This matters because the URL fetch (which takes a few seconds) happens during the request that inserts the link.


## Micro.blog Integration

Micro.blog can subscribe to an RSS or JSON feed. Once `links.joshuapsteele.com/feed.xml` is live, add it as a feed source in Micro.blog's account settings. Link posts will appear on social.joshuapsteele.com automatically with no further configuration.

Make sure each RSS item has a stable `<guid>` (use the permalink URL) so Micro.blog doesn't create duplicate posts if the feed is regenerated.


## Future Enhancements (Not for Initial Build)

These are ideas to revisit after the core is working. Don't build any of these in the first pass.

- Webmention support (send webmentions to the linked URLs).
- An Obsidian integration that syncs link entries to markdown files in a vault folder.
- A simple admin web UI for editing/deleting links from a browser (right now, use the API directly or add links via Drafts).
- Import from Pinboard, Raindrop, or browser bookmarks.
- Search (SQLite FTS5 makes this straightforward to add later).
- Displaying `og:image` thumbnails alongside link entries.
- A "link of the day" or "weekly digest" email feature.
- Fitness data dashboard on the same VPS (separate Go app, same server).


## Success Criteria

The project is "done" (for now) when:

1. I can open Drafts on my phone, write a URL and a sentence or two of commentary, tap an action, and see the link appear on links.joshuapsteele.com within a few seconds.
2. The site has a working RSS feed that Micro.blog can subscribe to.
3. The site looks clean, loads fast, and feels like it belongs alongside joshuapsteele.com.
4. The whole thing runs on a single DigitalOcean droplet with no external services.
5. I understand every piece of the stack from the Go source code to the systemd service file to the Caddy config.

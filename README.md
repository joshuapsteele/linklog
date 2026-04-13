# LinkLog

A personal link blog at [links.joshuapsteele.com](https://links.joshuapsteele.com). Save interesting URLs with short commentary from your phone (via [Drafts](https://getdrafts.com)) and publish them to a public feed with RSS. Links can also be created, edited, and deleted through a browser-based admin UI.

## How It Works

LinkLog is a single Go binary backed by SQLite. It serves a JSON API (for posting links from Drafts), a browser-based admin UI (for managing links), and public HTML pages (for reading them). Caddy sits in front as a reverse proxy and handles HTTPS automatically.

The typical posting workflow: open Drafts on iOS, write a URL and a sentence or two of commentary, tap the LinkLog action, and the link appears on the site within a few seconds. When a link is created, the server automatically fetches the target page to extract its title and asynchronously sends a webmention to the linked URL if it supports them.

```
[Drafts on iPhone]
        |
        | POST /api/links (Bearer token)
        v
[Caddy on VPS] --> [Go binary :8080] --> [SQLite file]
        |
        | serves HTML, RSS, JSON Feed
        v
[Browser / RSS reader / Micro.blog]
```

## Project Structure

```
linklog/
├── main.go              # Entry point, config, routing, embedded files
├── handlers.go          # HTTP handlers for API and public pages
├── admin.go             # Admin UI handlers (auth, create, edit, delete, webmention)
├── db.go                # SQLite initialization, migrations, queries
├── fetch.go             # URL metadata extraction (title, description)
├── webmention.go        # Webmention discovery and async sending
├── models.go            # Link struct, request/response types
├── templates/
│   ├── base.html        # Shared HTML shell (head, header, footer)
│   ├── feed.html        # Main link feed with pagination
│   ├── single.html      # Single link permalink page
│   ├── tag.html         # Tag-filtered feed
│   ├── about.html       # About page
│   ├── admin_base.html  # Admin UI shared shell
│   ├── admin_login.html # Admin login form
│   ├── admin_index.html # Admin link list with edit/delete/webmention controls
│   ├── admin_new.html   # Admin new link form
│   └── admin_edit.html  # Admin edit link form
├── static/
│   ├── style.css        # Public site CSS (matches joshuapsteele.com PaperMod theme)
│   └── admin.css        # Admin UI CSS
├── drafts/
│   └── linklog-action.js  # Drafts app action script
├── docs/
│   └── OPERATIONS.md    # Server operations and maintenance guide
├── go.mod
├── go.sum
├── Makefile
└── linklog-spec.md      # Original project specification
```

Templates and static assets are embedded into the binary at compile time using Go's `embed` package, so the deployed artifact is a single file with no external dependencies.

## Dependencies

Three external Go packages, all chosen to stay close to the standard library:

- `github.com/go-chi/chi/v5` — lightweight HTTP router with path parameters
- `modernc.org/sqlite` — pure-Go SQLite driver (no CGo, cross-compiles cleanly)
- `golang.org/x/net/html` — HTML tokenizer for extracting page metadata and discovering webmention endpoints

Everything else (templates, logging, JSON encoding, XML for RSS) uses the Go standard library.

## Configuration

All configuration is through environment variables. No config files.

| Variable | Required | Default | Description |
|---|---|---|---|
| `LINKLOG_API_TOKEN` | Yes | — | Bearer token for API authentication (Drafts action) |
| `LINKLOG_ADMIN_PASSWORD` | Yes | — | Password for the browser-based admin UI |
| `LINKLOG_DB_PATH` | No | `./linklog.db` | Path to the SQLite database file |
| `LINKLOG_PORT` | No | `8080` | Port the server listens on |
| `LINKLOG_BASE_URL` | No | `https://links.joshuapsteele.com` | Used for permalinks, feed URLs, and webmention source URLs |

## Local Development

```bash
# Install dependencies and build
go mod tidy
make build

# Run with development defaults
make run
```

This starts the server at `http://localhost:8080`. The SQLite database is created automatically on first run. The admin UI is at `http://localhost:8080/admin`.

Test the API:

```bash
# Create a link
curl -X POST http://localhost:8080/api/links \
  -H "Authorization: Bearer dev-token-change-me" \
  -H "Content-Type: application/json" \
  -d '{"url":"https://go.dev","commentary":"The Go homepage.","tags":"go"}'

# List all links
curl http://localhost:8080/api/links \
  -H "Authorization: Bearer dev-token-change-me"

# Update a link
curl -X PATCH http://localhost:8080/api/links/1 \
  -H "Authorization: Bearer dev-token-change-me" \
  -H "Content-Type: application/json" \
  -d '{"commentary":"Updated note."}'

# Delete a link
curl -X DELETE http://localhost:8080/api/links/1 \
  -H "Authorization: Bearer dev-token-change-me"
```

Visit `http://localhost:8080` in a browser to see the public feed, or `http://localhost:8080/feed.xml` for the RSS output.

## API Endpoints

All API routes require an `Authorization: Bearer <token>` header matching `LINKLOG_API_TOKEN`.

**POST /api/links** — Create a new link. The server fetches the URL to extract the page title automatically, then queues a webmention to the target URL. Request body:

```json
{
  "url": "https://example.com/article",
  "commentary": "Worth reading.",
  "tags": "webdev,go"
}
```

**GET /api/links** — List all links as JSON. Optional query parameters: `?tag=go`, `?published=false`, `?limit=50`.

**PATCH /api/links/{id}** — Partial update. Send only the fields you want to change.

**DELETE /api/links/{id}** — Delete a link. Returns 204 on success, 404 if not found.

## Public Pages

These are unauthenticated HTML pages:

- **GET /** — Main feed, 20 links per page, reverse chronological order
- **GET /link/{id}** — Permalink for a single link entry
- **GET /tag/{tag}** — Feed filtered to a specific tag
- **GET /about** — About page
- **GET /feed.xml** — RSS 2.0 feed (most recent 50 links)
- **GET /feed.json** — JSON Feed 1.1 (most recent 50 links)

## Admin UI

The admin UI lives at `/admin` and requires a password (set via `LINKLOG_ADMIN_PASSWORD`). Authentication is cookie-based with a 7-day session. The cookie is marked `Secure` when the base URL uses HTTPS.

- **GET /admin** — Dashboard listing all links with edit, delete, and webmention controls
- **GET /admin/login** — Login form
- **GET /admin/links/new** — Form to create a new link (with the same auto-fetch behavior as the API)
- **GET /admin/links/{id}/edit** — Form to edit a link's title, URL, commentary, tags, and published status
- **POST /admin/links/{id}/delete** — Delete a link (with a confirmation prompt in the browser)
- **POST /admin/links/{id}/webmention** — Manually retry sending a webmention for a link

## Webmention Support

When a link is created (via the API or admin UI), the server asynchronously attempts to send a webmention to the linked URL. The process:

1. Discover the webmention endpoint from the target page's HTTP `Link` header or HTML `<link rel="webmention">` tag.
2. POST `source` (the LinkLog permalink) and `target` (the linked URL) to that endpoint.
3. Record the result in the database as `sent`, `failed`, or `unsupported` (no endpoint found).

Webmention status is visible in the admin UI. Failed webmentions can be retried from the edit page or the admin dashboard. Sites that don't support webmentions show as `unsupported`, which is expected and not an error.

## Deployment

The Makefile includes a `deploy` target that cross-compiles for Linux, copies the binary to the server atomically, and restarts the service:

```bash
make deploy
```

The binary is copied as `linklog.new` first, then swapped into place with `mv` — this avoids the "file in use" error that would occur from overwriting a running process directly.

This assumes:
- An SSH config alias called `linklog-server` pointing to the DigitalOcean droplet
- The binary destination is `/opt/linklog/linklog`, owned by `joshuapsteele`
- A systemd service at `/etc/systemd/system/linklog.service` runs the binary as `joshuapsteele`
- `joshuapsteele` has passwordless sudo for `systemctl restart linklog` via `/etc/sudoers.d/linklog`
- Caddy is configured at `/etc/caddy/Caddyfile` to reverse-proxy `links.joshuapsteele.com` to `localhost:8080`
- Environment variables are set in `/opt/linklog/.env`, loaded by the systemd unit

See `docs/OPERATIONS.md` for server maintenance, log inspection, database backups, and troubleshooting.

## Drafts Action (iOS)

The primary mobile posting interface. Create a new draft in the Drafts app with this format:

```
https://example.com/interesting-article
go,indieweb,lean
This is a sharp take on keeping things simple.
```

Line 1 is the URL, line 2 is comma-separated tags (optional), and everything after that is your commentary. The action script at `drafts/linklog-action.js` handles parsing, API authentication, and posting. It stores the API token securely using Drafts' credential system.

The server automatically fetches the page title, so you don't need to include it in the draft.

## Micro.blog Integration

Add `https://links.joshuapsteele.com/feed.xml` as a feed source in your Micro.blog account settings. Each RSS item uses a stable permalink as its GUID, so regenerating the feed won't create duplicate posts.

## License

Personal project. Not currently licensed for reuse.

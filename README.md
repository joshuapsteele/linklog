# LinkLog

A personal link blog at [links.joshuapsteele.com](https://links.joshuapsteele.com). Save interesting URLs with short commentary from your phone (via [Drafts](https://getdrafts.com)) and publish them to a public feed with RSS.

## How It Works

LinkLog is a single Go binary backed by SQLite. It serves both a JSON API (for posting links) and public HTML pages (for reading them). Caddy sits in front as a reverse proxy on the server and handles HTTPS automatically.

The typical workflow: open Drafts on iOS, write a URL and a sentence or two of commentary, tap the LinkLog action, and the link appears on the site within a few seconds.

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

## Project Structure

```
linklog/
├── main.go              # Entry point, config, routing, embedded files
├── handlers.go          # HTTP handlers for API and public pages
├── db.go                # SQLite initialization, migrations, queries
├── fetch.go             # URL metadata extraction (title, description)
├── models.go            # Link struct, request/response types
├── templates/
│   ├── base.html        # Shared HTML shell (head, header, footer)
│   ├── feed.html        # Main link feed with pagination
│   ├── single.html      # Single link permalink page
│   └── tag.html         # Tag-filtered feed
├── static/
│   └── style.css        # Hand-written CSS, no frameworks
├── drafts/
│   └── linklog-action.js  # Drafts app action script
├── go.mod
├── go.sum
├── Makefile
└── linklog-spec.md      # Full project specification
```

Templates and static assets are embedded into the binary at compile time using Go's `embed` package, so the deployed artifact is a single file with no external dependencies.

## Dependencies

Three external Go packages, all chosen to stay close to the standard library:

- `github.com/go-chi/chi/v5` — lightweight HTTP router with path parameters
- `modernc.org/sqlite` — pure-Go SQLite driver (no CGo, cross-compiles cleanly)
- `golang.org/x/net/html` — HTML tokenizer for extracting page metadata

Everything else (templates, logging, JSON encoding, XML for RSS) uses the Go standard library.

## Configuration

All configuration is through environment variables. No config files.

| Variable | Required | Default | Description |
|---|---|---|---|
| `LINKLOG_API_TOKEN` | Yes | — | Bearer token for API authentication |
| `LINKLOG_DB_PATH` | No | `./linklog.db` | Path to the SQLite database file |
| `LINKLOG_PORT` | No | `8080` | Port the server listens on |
| `LINKLOG_BASE_URL` | No | `https://links.joshuapsteele.com` | Used for permalinks and feed URLs |

## Local Development

```bash
# Install dependencies and build
go mod tidy
make build

# Run with development defaults
make run
```

This starts the server at `http://localhost:8080` with the API token set to `dev-token-change-me`. The SQLite database is created automatically on first run.

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

# Delete a link
curl -X DELETE http://localhost:8080/api/links/1 \
  -H "Authorization: Bearer dev-token-change-me"
```

Visit `http://localhost:8080` in a browser to see the public feed, or `http://localhost:8080/feed.xml` for the RSS output.

## API Endpoints

All API routes require an `Authorization: Bearer <token>` header.

**POST /api/links** — Create a new link. The server fetches the URL to extract the page title automatically. Request body:

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
- **GET /feed.xml** — RSS 2.0 feed (most recent 50 links)
- **GET /feed.json** — JSON Feed 1.1 (most recent 50 links)

## Deployment

The Makefile includes a `deploy` target that cross-compiles for Linux, copies the binary to the server, and restarts the service:

```bash
make deploy
```

This assumes:
- An SSH config alias called `linklog-server` pointing to the DigitalOcean droplet
- The binary destination is `/opt/linklog/linklog`, owned by `joshuapsteele`
- A systemd service at `/etc/systemd/system/linklog.service` runs the binary as `joshuapsteele`
- `joshuapsteele` has passwordless sudo for `systemctl restart linklog` via `/etc/sudoers.d/linklog`
- Caddy is configured at `/etc/caddy/Caddyfile` to reverse-proxy `links.joshuapsteele.com` to `localhost:8080`

See `linklog-spec.md` for full server setup instructions including the Caddy config, systemd unit file, DNS setup, and backup cron job.

## Drafts Action (iOS)

The primary input method. Create a new draft in the Drafts app with this format:

```
https://example.com/interesting-article
go,indieweb,lean
This is a sharp take on keeping things simple.
```

Line 1 is the URL, line 2 is comma-separated tags (optional), and everything after that is your commentary. The action script at `drafts/linklog-action.js` handles parsing, API authentication, and posting. It stores the API token securely using Drafts' credential system.

## Micro.blog Integration

Add `https://links.joshuapsteele.com/feed.xml` as a feed source in your Micro.blog account settings. Each RSS item uses a stable permalink as its GUID, so regenerating the feed won't create duplicate posts.

## License

Personal project. Not currently licensed for reuse.

# LinkLog Operations Guide

A practical reference for inspecting, maintaining, and troubleshooting the LinkLog server. All commands assume you're working from your local machine unless noted otherwise.

---

## Quick Reference

```bash
# Connect
ssh linklog-server

# Service status
ssh linklog-server 'sudo systemctl status linklog'

# Live logs
ssh linklog-server 'sudo journalctl -u linklog -f'

# Deploy a new build
make deploy

# Backup the database
ssh linklog-server 'cp /opt/linklog/linklog.db /opt/linklog/backups/linklog-$(date +%Y%m%d-%H%M%S).db'

# Restart the service
ssh linklog-server 'sudo systemctl restart linklog'
```

---

## Connecting to the Server

**Server:** 159.203.190.26  
**User:** joshuapsteele  
**SSH alias:** `linklog-server` (configured in `~/.ssh/config`)

Your `~/.ssh/config` entry should look like:

```
Host linklog-server
    HostName 159.203.190.26
    User joshuapsteele
    IdentityFile ~/.ssh/your-key
```

Connect with:

```bash
ssh linklog-server
```

Once in, you're in your home directory (`/home/joshuapsteele`). The application lives at `/opt/linklog/`.

---

## Checking Service Health

LinkLog runs as a systemd service called `linklog`.

**Check status (one-liner from your Mac):**

```bash
ssh linklog-server 'sudo systemctl status linklog'
```

The output shows whether the service is `active (running)` or has failed, plus the last few log lines. Look for `Active: active (running)` — if you see `failed` or `inactive`, see the Troubleshooting section below.

**Check that the web server responds:**

```bash
curl -I https://links.joshuapsteele.com
```

A `200 OK` response means everything is up. If you get a connection error or 502, the app is probably down (Caddy is running, but it can't reach the Go process).

---

## Reading Logs

LinkLog uses structured logging via Go's `log/slog`. All output goes to the systemd journal.

**Follow live logs** (stream new entries as they arrive):

```bash
ssh linklog-server 'sudo journalctl -u linklog -f'
```

Press `Ctrl+C` to stop streaming.

**View the last 100 lines:**

```bash
ssh linklog-server 'sudo journalctl -u linklog -n 100'
```

**View logs from a specific time period:**

```bash
# Last hour
ssh linklog-server 'sudo journalctl -u linklog --since "1 hour ago"'

# Since midnight
ssh linklog-server 'sudo journalctl -u linklog --since today'

# A specific date range
ssh linklog-server 'sudo journalctl -u linklog --since "2025-01-01" --until "2025-01-02"'
```

**Filter for errors only:**

```bash
ssh linklog-server 'sudo journalctl -u linklog -p err'
```

Caddy (the reverse proxy / HTTPS layer) has its own logs:

```bash
ssh linklog-server 'sudo journalctl -u caddy -n 50'
```

---

## Deploying Updates

From your local `linklog/` directory:

```bash
# Build and deploy in one step
make deploy
```

This compiles a new binary, copies it to the server as `linklog.new`, atomically replaces the running binary with `mv`, and restarts the service. The atomic swap avoids the "file in use" error you'd get from overwriting a running binary directly.

To do it manually if `make deploy` fails:

```bash
# 1. Build
GOOS=linux GOARCH=amd64 go build -o linklog .

# 2. Copy to server (as a temp file)
scp linklog linklog-server:/opt/linklog/linklog.new

# 3. Swap and restart
ssh linklog-server 'mv /opt/linklog/linklog.new /opt/linklog/linklog && sudo systemctl restart linklog'

# 4. Verify
ssh linklog-server 'sudo systemctl status linklog'
```

If the new version crashes immediately on startup, roll back by redeploying the previous binary. Logs will tell you what went wrong:

```bash
ssh linklog-server 'sudo journalctl -u linklog -n 30'
```

---

## The Environment File

The service reads configuration from `/opt/linklog/.env`. To view or edit it:

```bash
ssh linklog-server 'cat /opt/linklog/.env'
ssh linklog-server 'nano /opt/linklog/.env'
```

The file contains:

```
LINKLOG_API_TOKEN=your-secret-token
LINKLOG_DB_PATH=/opt/linklog/linklog.db
LINKLOG_PORT=8080
LINKLOG_BASE_URL=https://links.joshuapsteele.com
```

After editing, restart the service to pick up changes:

```bash
ssh linklog-server 'sudo systemctl restart linklog'
```

Keep the API token secret — it's what protects the `/api/links` endpoint used by your Drafts action.

---

## Database Operations

LinkLog uses a single SQLite file at `/opt/linklog/linklog.db`.

### Inspecting the Database

SSH in and open the SQLite shell:

```bash
ssh linklog-server
sqlite3 /opt/linklog/linklog.db
```

Useful queries inside the SQLite shell:

```sql
-- Count all links
SELECT COUNT(*) FROM links;

-- Most recent 10 links
SELECT id, title, url, published, created_at FROM links ORDER BY created_at DESC LIMIT 10;

-- Drafts (unpublished)
SELECT id, title, url FROM links WHERE published = 0;

-- Links with failed webmentions
SELECT id, title, url, webmention_status FROM links WHERE webmention_status = 'failed';

-- All tags used
SELECT DISTINCT tags FROM links WHERE tags != '';
```

Type `.quit` to exit.

### Manual Backup

```bash
ssh linklog-server 'cp /opt/linklog/linklog.db /opt/linklog/backups/linklog-$(date +%Y%m%d-%H%M%S).db'
```

List existing backups:

```bash
ssh linklog-server 'ls -lh /opt/linklog/backups/'
```

### Automated Backups

A daily backup cron job should be set up under your user. To check:

```bash
ssh linklog-server 'crontab -l'
```

You should see something like:

```
0 3 * * * cp /opt/linklog/linklog.db /opt/linklog/backups/linklog-$(date +\%Y\%m\%d).db
```

If it's missing, add it with `crontab -e`.

### Restoring from Backup

Stop the service first to avoid writes during restore:

```bash
ssh linklog-server 'sudo systemctl stop linklog'
ssh linklog-server 'cp /opt/linklog/backups/linklog-20250101.db /opt/linklog/linklog.db'
ssh linklog-server 'sudo systemctl start linklog'
```

Replace `linklog-20250101.db` with the backup filename you want to restore.

### Copying the Database to Your Mac

Useful for local inspection or archiving:

```bash
scp linklog-server:/opt/linklog/linklog.db ./linklog-backup-$(date +%Y%m%d).db
```

---

## systemd Service Management

The service definition lives at `/etc/systemd/system/linklog.service`. You shouldn't need to edit it often, but here's how to work with it.

**Common commands (run from your Mac):**

```bash
# Start
ssh linklog-server 'sudo systemctl start linklog'

# Stop
ssh linklog-server 'sudo systemctl stop linklog'

# Restart
ssh linklog-server 'sudo systemctl restart linklog'

# Reload after editing .env or the .service file
ssh linklog-server 'sudo systemctl daemon-reload && sudo systemctl restart linklog'

# Enable autostart on boot (should already be set)
ssh linklog-server 'sudo systemctl enable linklog'

# View the service definition
ssh linklog-server 'cat /etc/systemd/system/linklog.service'
```

---

## Caddy (HTTPS / Reverse Proxy)

Caddy handles HTTPS certificates and forwards traffic to the Go app on port 8080.

**Check Caddy status:**

```bash
ssh linklog-server 'sudo systemctl status caddy'
```

**View the Caddyfile:**

```bash
ssh linklog-server 'sudo cat /etc/caddy/Caddyfile'
```

It should contain something like:

```
links.joshuapsteele.com {
    reverse_proxy localhost:8080
}
```

**Reload Caddy after config changes:**

```bash
ssh linklog-server 'sudo systemctl reload caddy'
```

Caddy automatically renews Let's Encrypt certificates. If HTTPS stops working, check Caddy logs:

```bash
ssh linklog-server 'sudo journalctl -u caddy -n 50'
```

---

## OS Updates

It's good practice to apply security updates every few months.

```bash
ssh linklog-server
sudo apt update
sudo apt upgrade -y
sudo apt autoremove -y
```

If a kernel update is installed, you'll want to reboot. The service will come back up automatically since it's enabled in systemd:

```bash
sudo reboot
```

Wait 30–60 seconds, then reconnect and verify:

```bash
ssh linklog-server 'sudo systemctl status linklog'
```

---

## DigitalOcean Dashboard

Log in at [cloud.digitalocean.com](https://cloud.digitalocean.com). Your droplet is listed under **Droplets** and named something like `linklog` or `ubuntu-s-...`.

From the dashboard you can:

- **Monitor** CPU, memory, and bandwidth graphs under the droplet's "Graphs" tab
- **Access the console** if SSH is locked out (Droplet → Access → Launch Droplet Console)
- **Take a snapshot** for a full disk backup (Droplet → Backups & Snapshots → Take Snapshot — this briefly powers off the droplet)
- **Resize** if you need more resources
- **View bandwidth** to track data transfer (the cheapest droplet tier includes 1TB/month)

The Recovery Console in the DigitalOcean dashboard is your safety net if you ever lock yourself out of SSH — use it to fix the sshd config or reset passwords.

---

## Troubleshooting

### Service won't start

```bash
ssh linklog-server 'sudo journalctl -u linklog -n 50'
```

Common causes:
- Missing or incorrect `LINKLOG_API_TOKEN` in `/opt/linklog/.env`
- Database file permissions (should be owned by `joshuapsteele`)
- Port 8080 already in use — check with `sudo ss -tlnp | grep 8080`
- Binary compiled for the wrong architecture (must be `linux/amd64`)

### 502 Bad Gateway

Caddy is running but can't reach the Go app on port 8080. Check if the linklog service is actually running:

```bash
ssh linklog-server 'sudo systemctl status linklog'
```

Then look at logs for crash details:

```bash
ssh linklog-server 'sudo journalctl -u linklog -n 30'
```

### Site returns 404 or wrong content

Caddy might be serving something other than LinkLog. Check the Caddyfile:

```bash
ssh linklog-server 'sudo cat /etc/caddy/Caddyfile'
```

### Can't SSH in

1. Try the DigitalOcean Recovery Console (cloud.digitalocean.com → Droplet → Access)
2. Check if your IP changed (if you're using IP-based firewall rules)
3. From the console, verify `sshd` is running: `sudo systemctl status ssh`

### Database is locked / write errors

SQLite uses WAL mode, which handles most concurrent access gracefully. If you see "database is locked" errors, something may have crashed mid-write. Stop the service, make a backup, then restart:

```bash
ssh linklog-server 'sudo systemctl stop linklog'
ssh linklog-server 'cp /opt/linklog/linklog.db /opt/linklog/backups/linklog-emergency.db'
ssh linklog-server 'sqlite3 /opt/linklog/linklog.db "PRAGMA integrity_check;"'
ssh linklog-server 'sudo systemctl start linklog'
```

### Webmentions aren't sending

Check the link's webmention status in the admin UI at `https://links.joshuapsteele.com/admin`. If it shows "failed", click the webmention button to retry. You can also check logs for details:

```bash
ssh linklog-server 'sudo journalctl -u linklog | grep -i webmention'
```

Some sites don't support webmentions (status will show "unsupported") — that's expected and not an error.

### Disk space

Check available disk space:

```bash
ssh linklog-server 'df -h'
```

The SQLite database and backups are the main consumers. Old backups in `/opt/linklog/backups/` can be deleted manually if space is tight:

```bash
ssh linklog-server 'ls -lh /opt/linklog/backups/'
ssh linklog-server 'rm /opt/linklog/backups/linklog-20240101.db'
```

---

## File Layout on the Server

```
/opt/linklog/
├── linklog          # the compiled binary
├── linklog.db       # SQLite database
├── .env             # environment variables (keep secret)
└── backups/         # manual and cron backups
    └── linklog-20250101.db

/etc/systemd/system/linklog.service   # systemd unit
/etc/caddy/Caddyfile                   # Caddy reverse proxy config
```

The binary embeds all templates and static files, so there are no separate asset directories to manage on the server.

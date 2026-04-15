.PHONY: build run deploy clean

# Build for the current platform.
build:
	go build -o linklog .

# Run locally for development.
run: build
	LINKLOG_API_TOKEN=dev-token-change-me \
	LINKLOG_ADMIN_PASSWORD=dev-admin-password-change-me \
	LINKLOG_DB_PATH=./linklog.db \
	LINKLOG_BASE_URL=http://localhost:8080 \
	./linklog

# Cross-compile for Linux and deploy to the server.
# Copies to a temp file first, then moves into place, so the running
# process doesn't hold a lock on the destination binary.
deploy:
	GOOS=linux GOARCH=amd64 go build -o linklog .
	scp linklog linklog-server:/opt/linklog/linklog.new
	ssh linklog-server 'mv /opt/linklog/linklog.new /opt/linklog/linklog && sudo systemctl restart linklog'
	@echo "Deployed."

# Remove build artifacts.
clean:
	rm -f linklog linklog.db

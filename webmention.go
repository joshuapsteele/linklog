package main

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// SendWebmentionAsync discovers the webmention endpoint for targetURL, sends a
// webmention from sourceURL, and updates the link's status in the DB.
// Runs in a goroutine so it never blocks the request that created the link.
func SendWebmentionAsync(db *DB, linkID int64, targetURL, sourceURL string) {
	go func() {
		endpoint := DiscoverWebmentionEndpoint(targetURL)
		if endpoint == "" {
			slog.Info("no webmention endpoint", "link_id", linkID, "target", targetURL)
			if err := db.UpdateWebmentionStatus(linkID, "unsupported", ""); err != nil {
				slog.Error("failed to update webmention status", "error", err)
			}
			return
		}

		if err := SendWebmention(endpoint, sourceURL, targetURL); err != nil {
			slog.Warn("webmention failed", "link_id", linkID, "target", targetURL, "error", err)
			if err := db.UpdateWebmentionStatus(linkID, "failed", endpoint); err != nil {
				slog.Error("failed to update webmention status", "error", err)
			}
			return
		}

		slog.Info("webmention sent", "link_id", linkID, "target", targetURL, "endpoint", endpoint)
		if err := db.UpdateWebmentionStatus(linkID, "sent", endpoint); err != nil {
			slog.Error("failed to update webmention status", "error", err)
		}
	}()
}

// DiscoverWebmentionEndpoint tries to find the webmention endpoint for a URL.
// It checks HTTP Link headers first, then <link> and <a> tags in the HTML.
// Returns "" if no endpoint is found or if the request fails.
func DiscoverWebmentionEndpoint(targetURL string) string {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", targetURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "LinkLog/1.0 (+https://links.joshuapsteele.com)")

	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	// Check HTTP Link headers first — they're cheaper than parsing HTML.
	for _, header := range resp.Header["Link"] {
		if endpoint := parseLinkHeader(header, "webmention"); endpoint != "" {
			return resolveURL(targetURL, endpoint)
		}
	}

	// Fall back to HTML body.
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") && !strings.Contains(ct, "application/xhtml") {
		return ""
	}
	limited := io.LimitReader(resp.Body, 1<<20) // 1MB cap
	return discoverFromHTML(limited, targetURL)
}

// SendWebmention posts a webmention from sourceURL to targetURL via endpoint.
func SendWebmention(endpoint, sourceURL, targetURL string) error {
	client := &http.Client{Timeout: 10 * time.Second}

	body := url.Values{
		"source": {sourceURL},
		"target": {targetURL},
	}.Encode()

	req, err := http.NewRequest("POST", endpoint, strings.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "LinkLog/1.0 (+https://links.joshuapsteele.com)")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send: %w", err)
	}
	defer resp.Body.Close()

	// Webmention spec accepts 200, 201, and 202.
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("endpoint returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// parseLinkHeader looks for a URL with the given rel in an HTTP Link header value.
// Example: </webmention>; rel="webmention"
func parseLinkHeader(header, rel string) string {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		segments := strings.SplitN(part, ";", 2)
		if len(segments) < 2 {
			continue
		}
		rawURL := strings.TrimSpace(segments[0])
		if !strings.HasPrefix(rawURL, "<") || !strings.HasSuffix(rawURL, ">") {
			continue
		}
		linkURL := rawURL[1 : len(rawURL)-1]

		for _, param := range strings.Split(segments[1], ";") {
			param = strings.TrimSpace(param)
			if !strings.HasPrefix(param, "rel=") {
				continue
			}
			relVal := strings.Trim(strings.TrimPrefix(param, "rel="), `"' `)
			for _, r := range strings.Fields(relVal) {
				if r == rel {
					return linkURL
				}
			}
		}
	}
	return ""
}

// discoverFromHTML scans the HTML for <link rel="webmention"> or <a rel="webmention">.
func discoverFromHTML(r io.Reader, baseURL string) string {
	tz := html.NewTokenizer(r)
	for {
		tt := tz.Next()
		switch tt {
		case html.ErrorToken:
			return ""
		case html.StartTagToken, html.SelfClosingTagToken:
			tn, hasAttr := tz.TagName()
			if !hasAttr {
				continue
			}
			tagName := string(tn)
			if tagName == "body" {
				// Webmention endpoints are declared in <head>; stop scanning.
				return ""
			}
			if tagName != "link" && tagName != "a" {
				continue
			}
			attrs := collectAttrs(tz) // defined in fetch.go, same package
			href := attrs["href"]
			if href == "" {
				continue
			}
			for _, r := range strings.Fields(strings.ToLower(attrs["rel"])) {
				if r == "webmention" {
					return resolveURL(baseURL, href)
				}
			}
		}
	}
}

// resolveURL resolves ref relative to base.
func resolveURL(base, ref string) string {
	baseU, err := url.Parse(base)
	if err != nil {
		return ref
	}
	refU, err := url.Parse(ref)
	if err != nil {
		return ref
	}
	return baseU.ResolveReference(refU).String()
}

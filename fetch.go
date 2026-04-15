package main

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// PageMeta holds metadata extracted from a fetched URL.
type PageMeta struct {
	Title        string
	Description  string
	SiteName     string
	ImageURL     string
	CanonicalURL string
}

// FetchPageMeta fetches the URL and extracts title and description metadata.
// It never returns an error that should block link creation. If anything goes wrong,
// it returns a PageMeta with the raw URL as the title.
func FetchPageMeta(rawURL string) PageMeta {
	fallback := PageMeta{Title: rawURL}

	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	req, err := http.NewRequest("GET", rawURL, nil)
	if err != nil {
		return fallback
	}
	req.Header.Set("User-Agent", "LinkLog/1.0 (+https://links.joshuapsteele.com)")
	req.Header.Set("Accept", "text/html")

	resp, err := client.Do(req)
	if err != nil {
		return fallback
	}
	defer resp.Body.Close()

	// Only parse HTML responses.
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") && !strings.Contains(ct, "application/xhtml") {
		return fallback
	}

	// Limit how much we read to avoid pulling in huge pages.
	limited := io.LimitReader(resp.Body, 1<<20) // 1MB
	return extractMeta(limited, rawURL)
}

func extractMeta(r io.Reader, fallbackTitle string) PageMeta {
	meta := PageMeta{Title: fallbackTitle}

	tokenizer := html.NewTokenizer(r)
	var inTitle bool
	var titleText string
	var ogTitle, ogDescription, metaDescription string
	var siteName, imageURL, canonicalURL string

	for {
		tt := tokenizer.Next()
		switch tt {
		case html.ErrorToken:
			// End of document or error; use what we have.
			return resolveMeta(meta, fallbackTitle, titleText, ogTitle, ogDescription, metaDescription, siteName, imageURL, canonicalURL)

		case html.StartTagToken, html.SelfClosingTagToken:
			tn, hasAttr := tokenizer.TagName()
			tagName := string(tn)

			if tagName == "title" {
				inTitle = true
				continue
			}

			if tagName == "meta" && hasAttr {
				attrs := collectAttrs(tokenizer)
				name := strings.ToLower(attrs["name"])
				property := strings.ToLower(attrs["property"])
				content := attrs["content"]

				switch {
				case property == "og:title":
					ogTitle = content
				case property == "og:description":
					ogDescription = content
				case property == "og:site_name":
					siteName = content
				case property == "og:image":
					imageURL = content
				case name == "description":
					metaDescription = content
				}
			}

			if tagName == "link" && hasAttr {
				attrs := collectAttrs(tokenizer)
				for _, rel := range strings.Fields(strings.ToLower(attrs["rel"])) {
					if rel == "canonical" {
						canonicalURL = attrs["href"]
						break
					}
				}
			}

			// Stop parsing once we hit the body; meta tags are in the head.
			if tagName == "body" {
				return resolveMeta(meta, fallbackTitle, titleText, ogTitle, ogDescription, metaDescription, siteName, imageURL, canonicalURL)
			}

		case html.TextToken:
			if inTitle {
				titleText += string(tokenizer.Text())
			}

		case html.EndTagToken:
			tn, _ := tokenizer.TagName()
			if string(tn) == "title" {
				inTitle = false
			}
		}
	}
}

func resolveMeta(meta PageMeta, baseURL, titleText, ogTitle, ogDescription, metaDescription, siteName, imageURL, canonicalURL string) PageMeta {
	// Prefer og:title over <title>.
	switch {
	case ogTitle != "":
		meta.Title = strings.TrimSpace(ogTitle)
	case strings.TrimSpace(titleText) != "":
		meta.Title = strings.TrimSpace(titleText)
	}

	// Prefer og:description over meta description.
	switch {
	case ogDescription != "":
		meta.Description = strings.TrimSpace(ogDescription)
	case metaDescription != "":
		meta.Description = strings.TrimSpace(metaDescription)
	}

	meta.SiteName = strings.TrimSpace(siteName)
	if strings.TrimSpace(imageURL) != "" {
		meta.ImageURL = resolveURL(baseURL, strings.TrimSpace(imageURL))
	}
	if strings.TrimSpace(canonicalURL) != "" {
		meta.CanonicalURL = resolveURL(baseURL, strings.TrimSpace(canonicalURL))
	}

	return meta
}

func collectAttrs(t *html.Tokenizer) map[string]string {
	attrs := make(map[string]string)
	for {
		key, val, more := t.TagAttr()
		attrs[string(key)] = string(val)
		if !more {
			break
		}
	}
	return attrs
}

package main

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
)

// Link represents a single saved link entry.
type Link struct {
	ID                 int64     `json:"id"`
	URL                string    `json:"url"`
	Title              string    `json:"title"`
	Commentary         string    `json:"commentary"`
	Tags               string    `json:"tags"`
	Description        string    `json:"description"`
	SiteName           string    `json:"site_name"`
	ImageURL           string    `json:"image_url"`
	CanonicalURL       string    `json:"canonical_url"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	Published          bool      `json:"published"`
	Pinned             bool      `json:"pinned"`
	WebmentionStatus   string    `json:"webmention_status"`             // pending, sent, failed, unsupported
	WebmentionEndpoint string    `json:"webmention_endpoint,omitempty"` // discovered endpoint URL
}

// TagList returns the tags split into a slice, filtering out empty strings.
func (l Link) TagList() []string {
	return SplitTags(l.Tags)
}

// SplitTags returns normalized tag names from a comma-separated tag string.
func SplitTags(value string) []string {
	if value == "" {
		return nil
	}
	raw := strings.Split(value, ",")
	tags := make([]string, 0, len(raw))
	for _, t := range raw {
		t = NormalizeTag(t)
		if t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}

// NormalizeTags returns a canonical comma-separated tag string.
func NormalizeTags(value string) string {
	tags := SplitTags(value)
	if len(tags) == 0 {
		return ""
	}
	seen := make(map[string]bool, len(tags))
	normalized := make([]string, 0, len(tags))
	for _, tag := range tags {
		if !seen[tag] {
			seen[tag] = true
			normalized = append(normalized, tag)
		}
	}
	return strings.Join(normalized, ",")
}

// NormalizeTag returns the canonical form for a single tag.
func NormalizeTag(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(value, "#"))
	if value == "" {
		return ""
	}

	var b strings.Builder
	lastHyphen := false
	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			lastHyphen = false
		case r == '-' || r == '_' || r == '.':
			if !lastHyphen {
				b.WriteRune(r)
				lastHyphen = r == '-'
			}
		case unicode.IsSpace(r):
			if b.Len() > 0 && !lastHyphen {
				b.WriteRune('-')
				lastHyphen = true
			}
		default:
			if b.Len() > 0 && !lastHyphen {
				b.WriteRune('-')
				lastHyphen = true
			}
		}
	}

	return strings.Trim(b.String(), "-_.")
}

// RelativeTime returns a human-friendly relative timestamp.
func (l Link) RelativeTime() string {
	now := time.Now().UTC()
	d := now.Sub(l.CreatedAt)

	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(math.Floor(d.Minutes()))
		if m == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	case d < 24*time.Hour:
		h := int(math.Floor(d.Hours()))
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	case d < 48*time.Hour:
		return "yesterday"
	case d < 7*24*time.Hour:
		days := int(math.Floor(d.Hours() / 24))
		return fmt.Sprintf("%d days ago", days)
	default:
		return l.CreatedAt.Format("January 2, 2006")
	}
}

// CreateLinkRequest is the JSON body for POST /api/links.
type CreateLinkRequest struct {
	URL        string `json:"url"`
	Commentary string `json:"commentary"`
	Tags       string `json:"tags"`
	Pinned     bool   `json:"pinned"`
}

// CreateLinkResponse is returned by POST /api/links.
type CreateLinkResponse struct {
	Status    string `json:"status"`
	Duplicate bool   `json:"duplicate"`
	Message   string `json:"message"`
	Permalink string `json:"permalink"`
	AdminURL  string `json:"admin_url"`
	Link      *Link  `json:"link"`
}

// UpdateLinkRequest is the JSON body for PATCH /api/links/{id}.
// All fields are pointers so we can distinguish "not provided" from "set to zero value".
type UpdateLinkRequest struct {
	URL          *string `json:"url"`
	Title        *string `json:"title"`
	Commentary   *string `json:"commentary"`
	Tags         *string `json:"tags"`
	Description  *string `json:"description"`
	SiteName     *string `json:"site_name"`
	ImageURL     *string `json:"image_url"`
	CanonicalURL *string `json:"canonical_url"`
	Published    *bool   `json:"published"`
	Pinned       *bool   `json:"pinned"`
}

package main

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// Link represents a single saved link entry.
type Link struct {
	ID                 int64     `json:"id"`
	URL                string    `json:"url"`
	Title              string    `json:"title"`
	Commentary         string    `json:"commentary"`
	Tags               string    `json:"tags"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	Published          bool      `json:"published"`
	Pinned             bool      `json:"pinned"`
	WebmentionStatus   string    `json:"webmention_status"`             // pending, sent, failed, unsupported
	WebmentionEndpoint string    `json:"webmention_endpoint,omitempty"` // discovered endpoint URL
}

// TagList returns the tags split into a slice, filtering out empty strings.
func (l Link) TagList() []string {
	if l.Tags == "" {
		return nil
	}
	raw := strings.Split(l.Tags, ",")
	tags := make([]string, 0, len(raw))
	for _, t := range raw {
		t = strings.TrimSpace(t)
		if t != "" {
			tags = append(tags, t)
		}
	}
	return tags
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

// UpdateLinkRequest is the JSON body for PATCH /api/links/{id}.
// All fields are pointers so we can distinguish "not provided" from "set to zero value".
type UpdateLinkRequest struct {
	URL        *string `json:"url"`
	Title      *string `json:"title"`
	Commentary *string `json:"commentary"`
	Tags       *string `json:"tags"`
	Published  *bool   `json:"published"`
	Pinned     *bool   `json:"pinned"`
}

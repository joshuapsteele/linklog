package main

import (
	"html/template"
	"testing"
)

func TestPublicTemplatesParse(t *testing.T) {
	funcMap := template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"sub": func(a, b int) int { return a - b },
	}

	for _, page := range []string{"feed.html", "single.html", "tag.html", "tags.html", "search.html", "about.html"} {
		if _, err := template.New("").Funcs(funcMap).ParseFS(templateFS, "templates/base.html", "templates/"+page); err != nil {
			t.Fatalf("parse %s: %v", page, err)
		}
	}
}

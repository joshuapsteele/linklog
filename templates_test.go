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

func TestAdminTemplatesParse(t *testing.T) {
	for _, page := range []string{"admin_login.html", "admin_index.html", "admin_edit.html", "admin_new.html"} {
		if _, err := template.New("").ParseFS(templateFS, "templates/admin_base.html", "templates/"+page); err != nil {
			t.Fatalf("parse %s: %v", page, err)
		}
	}
}

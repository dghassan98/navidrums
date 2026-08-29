package httpapp

import (
	"html/template"
	"path/filepath"
	"testing"

	"github.com/cesargomez89/navidrums/web"
)

// TestTemplatesParse guards against a find-and-replace leaving a template
// unparseable, which surfaces only as a 500 at request time.
func TestTemplatesParse(t *testing.T) {
	pages, err := filepath.Glob("../../web/templates/*.html")
	if err != nil || len(pages) == 0 {
		t.Fatalf("no templates found: %v", err)
	}

	for _, page := range pages {
		name := filepath.Base(page)
		t.Run(name, func(t *testing.T) {
			tmpl := template.New("base").Funcs(templateFuncs())
			patterns := []string{
				"templates/base.html",
				"templates/" + name,
				"templates/search_results.html",
				"templates/components/*.html",
			}
			if _, err := tmpl.ParseFS(web.Files, patterns...); err != nil {
				t.Errorf("failed to parse: %v", err)
			}
		})
	}
}

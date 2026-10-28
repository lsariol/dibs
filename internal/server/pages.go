package server

import (
	"bytes"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"

	"github.com/lsariol/dibs/web"
)

// pages holds one parsed template set per page, each with the shared layout.
type pages struct {
	version string
	sets    map[string]*template.Template
}

// pageData is what every page template gets.
type pageData struct {
	Version string
	Data    any
}

var templateFuncs = template.FuncMap{
	"seq": func(from, to int) []int {
		var out []int
		for i := from; i <= to; i++ {
			out = append(out, i)
		}
		return out
	},
}

func loadPages(version string) (*pages, error) {
	names, err := fs.Glob(web.FS, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("server: templates: %w", err)
	}
	p := &pages{version: version, sets: map[string]*template.Template{}}
	for _, n := range names {
		name := path.Base(n)
		name = name[:len(name)-len(".html")]
		if name == "layout" {
			continue
		}
		t, err := template.New(name).Funcs(templateFuncs).ParseFS(web.FS, "templates/layout.html", n)
		if err != nil {
			return nil, fmt.Errorf("server: template %s: %w", name, err)
		}
		p.sets[name] = t
	}
	return p, nil
}

// render writes a full page; a template error becomes a plain 500.
func (s *Server) render(w http.ResponseWriter, status int, name string, data any) {
	t, ok := s.pages.sets[name]
	if !ok {
		s.log.Error("server: unknown page template", "name", name)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "layout", pageData{Version: s.pages.version, Data: data}); err != nil {
		s.log.Error("server: rendering page", "name", name, "err", err)
		http.Error(w, "Internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

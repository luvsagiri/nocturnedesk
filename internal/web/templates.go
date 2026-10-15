package web

import (
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

type TemplateRenderer struct {
	fs        fs.FS
	templates map[string]*template.Template
	mu        sync.RWMutex
	funcs     template.FuncMap
}

func NewTemplateRenderer(templatesFS fs.FS) *TemplateRenderer {
	r := &TemplateRenderer{
		fs:        templatesFS,
		templates: make(map[string]*template.Template),
		funcs: template.FuncMap{
			"firstChar": func(s string) string {
				s = strings.TrimSpace(s)
				if s == "" {
					return "?"
				}
				r, _ := utf8.DecodeRuneInString(s)
				return strings.ToUpper(string(r))
			},
			"safeHTML": func(s string) template.HTML {
				return template.HTML(s)
			},
			"formatDate": func(s string) string {
				if len(s) >= 10 {
					return s[:10]
				}
				return s
			},
			"formatDateTime": func(s string) string {
				if t, err := time.Parse(time.RFC3339, s); err == nil {
					return t.Local().Format("2006-01-02 15:04")
				}
				if len(s) >= 16 {
					return strings.Replace(s[:16], "T", " ", 1)
				}
				return s
			},
			"humanizeBytes": func(b int64) string {
				const unit = 1024
				if b < unit {
					return fmt.Sprintf("%d B", b)
				}
				div, exp := int64(unit), 0
				for n := b / unit; n >= unit; n /= unit {
					div *= unit
					exp++
				}
				return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
			},
			"isOverdue": func(dueDate string) bool {
				if strings.TrimSpace(dueDate) == "" {
					return false
				}
				today := time.Now().Format("2006-01-02")
				return dueDate < today
			},
			"add": func(a, b int) int {
				return a + b
			},
			"sub": func(a, b int) int {
				return a - b
			},
		},
	}
	return r
}

func (r *TemplateRenderer) Render(w http.ResponseWriter, name string, data any) error {
	r.mu.RLock()
	tmpl, exists := r.templates[name]
	r.mu.RUnlock()

	if !exists {
		r.mu.Lock()
		defer r.mu.Unlock()

		// Double check under write lock
		if tmpl, exists = r.templates[name]; !exists {
			var err error
			// If layout.html exists in FS, parse layout.html + name.html
			pagePath := filepath.ToSlash(filepath.Join("templates", name+".html"))
			layoutPath := filepath.ToSlash("templates/layout.html")

			t := template.New("layout.html").Funcs(r.funcs)
			t, err = t.ParseFS(r.fs, layoutPath, pagePath)
			if err != nil {
				// Fallback to single page template if layout fails
				t = template.New(name + ".html").Funcs(r.funcs)
				t, err = t.ParseFS(r.fs, pagePath)
				if err != nil {
					return fmt.Errorf("failed to parse template %s: %w", name, err)
				}
			}
			r.templates[name] = t
			tmpl = t
		}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return tmpl.Execute(w, data)
}

func (r *TemplateRenderer) RenderPartial(w http.ResponseWriter, name string, data any) error {
	pagePath := filepath.ToSlash(filepath.Join("templates", name+".html"))
	t, err := template.New(name + ".html").Funcs(r.funcs).ParseFS(r.fs, pagePath)
	if err != nil {
		return fmt.Errorf("failed to parse partial %s: %w", name, err)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return t.Execute(w, data)
}

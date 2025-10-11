package server

import (
	"html/template"
	"log/slog"
	"net/http"
	"path/filepath"
)

// TemplateRenderer holds parsed HTML templates
type TemplateRenderer struct {
	templates *template.Template
	logger    *slog.Logger
}

// NewTemplateRenderer creates a new template renderer
func NewTemplateRenderer(templatesDir string, logger *slog.Logger) (*TemplateRenderer, error) {
	// Parse all templates in the directory
	tmpl, err := template.ParseGlob(filepath.Join(templatesDir, "*.html"))
	if err != nil {
		return nil, err
	}

	return &TemplateRenderer{
		templates: tmpl,
		logger:    logger,
	}, nil
}

// Render renders a template with the given data
func (tr *TemplateRenderer) Render(w http.ResponseWriter, name string, data interface{}) error {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	return tr.templates.ExecuteTemplate(w, name, data)
}

// handleSSEClientPage serves the SSE client demo page
func handleSSEClientPage(renderer *TemplateRenderer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := renderer.Render(w, "sse-client.html", nil); err != nil {
			renderer.logger.Error("failed to render template", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	}
}

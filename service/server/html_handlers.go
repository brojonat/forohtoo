package server

import (
	"embed"
	"html/template"
	"log/slog"
	"net/http"
	"os"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static/*
var staticFS embed.FS

// TemplateRenderer holds parsed HTML templates
type TemplateRenderer struct {
	templates *template.Template
	logger    *slog.Logger
}

// NewTemplateRenderer creates a new template renderer from embedded files
func NewTemplateRenderer(logger *slog.Logger) (*TemplateRenderer, error) {
	// Parse all templates from embedded filesystem
	tmpl, err := template.ParseFS(templatesFS, "templates/*.html")
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
		data := map[string]interface{}{
			"USDCMainnetMintAddress": os.Getenv("USDC_MAINNET_MINT_ADDRESS"),
			"USDCDevnetMintAddress":  os.Getenv("USDC_DEVNET_MINT_ADDRESS"),
		}
		if err := renderer.Render(w, "sse-client.html", data); err != nil {
			renderer.logger.Error("failed to render template", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			return
		}
	}
}

// handleFavicon serves the favicon from embedded static files
func handleFavicon() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data, err := staticFS.ReadFile("static/favicon.svg")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/svg+xml")
		w.Header().Set("Cache-Control", "public, max-age=31536000")
		w.Write(data)
	}
}

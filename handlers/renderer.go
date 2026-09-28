package handlers

import (
	"embed"
	"html/template"
	"io"

	"github.com/labstack/echo/v4"
)

//go:embed templates/*.html
var templateFS embed.FS

// Renderer lets handlers call c.Render with the embedded HTML templates.
// Set it once on the server: e.Renderer = handlers.NewRenderer().
type Renderer struct {
	templates *template.Template
}

func NewRenderer() *Renderer {
	return &Renderer{templates: template.Must(template.ParseFS(templateFS, "templates/*.html"))}
}

func (r *Renderer) Render(w io.Writer, name string, data any, _ echo.Context) error {
	return r.templates.ExecuteTemplate(w, name, data)
}

// Package routes maps URLs to handlers.
package routes

import (
	"net/http"

	"github.com/bulutaysarac/pastebin-system-design/handlers"
)

func New(h *handlers.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", h.Home)           // home page with the form
	mux.HandleFunc("POST /paste", h.CreatePaste) // form submit → redirect to /{id}
	mux.HandleFunc("GET /{id}", h.ShowPaste)     // read a paste
	return mux
}

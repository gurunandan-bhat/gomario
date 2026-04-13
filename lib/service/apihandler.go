package service

import (
	"fmt"
	"net/http"
)

// apiHandler is the handler type for JSON API routes. It behaves like
// serviceHandler but returns a JSON 500 on unhandled errors instead of plain
// text, keeping error responses consistent within the /api/ surface.
type apiHandler func(w http.ResponseWriter, r *http.Request) error

func (h apiHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h(w, r); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"message":%q}`, err.Error())
	}
}

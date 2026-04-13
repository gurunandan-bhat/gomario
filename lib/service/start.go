package service

import (
	"net/http"

	"github.com/justinas/nosurf"
)

func (s *Service) start(w http.ResponseWriter, r *http.Request) error {

	data := struct {
		Title     string
		CSRFToken string
	}{
		Title:     "Start Here",
		CSRFToken: nosurf.Token(r),
	}
	return s.render(w, "start.go.html", data, nil, http.StatusOK)
}

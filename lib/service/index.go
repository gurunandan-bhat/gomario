package service

import (
	"net/http"
)

func (s *Service) index(w http.ResponseWriter, r *http.Request) error {

	return s.render(w, "index.go.html", nil, nil, http.StatusOK)
}

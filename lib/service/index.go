package service

import (
	"net/http"
)

func (s *Service) index(w http.ResponseWriter, r *http.Request) error {
	data := s.newTemplateData(r)
	data.Title = "Home"
	return s.render(w, "index.go.html", data, nil, http.StatusOK)
}

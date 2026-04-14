package service

import (
	"net/http"
)

func (s *Service) start(w http.ResponseWriter, r *http.Request) error {
	data := s.newTemplateData(r)
	data.Title = "Start Here"
	return s.render(w, "start.go.html", data, nil, http.StatusOK)
}

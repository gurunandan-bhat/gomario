package service

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"path/filepath"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

func newTemplateCache(templateRoot string) (map[string]*template.Template, error) {

	cache := map[string]*template.Template{}
	pages, err := filepath.Glob(templateRoot + "/pages/*.go.html")
	if err != nil {
		return nil, fmt.Errorf("error generating list of templates in pages: %w", err)
	}

	for _, page := range pages {

		name := filepath.Base(page)
		files := []string{
			templateRoot + "/common/base.go.html",
			templateRoot + "/common/head.go.html",
			templateRoot + "/common/top-menu.go.html",
			templateRoot + "/common/footer.go.html",
			templateRoot + "/common/js-includes.go.html",
			page,
		}

		tSet := template.New("mario-ai").Funcs(templateFuncs)
		tSet, err := tSet.ParseFiles(files...)
		if err != nil {
			return nil, fmt.Errorf("error creating template set for %s: %w", page, err)
		}
		tSet, err = tSet.ParseGlob(templateRoot + "/includes/*.go.html")
		if err != nil {
			return nil, fmt.Errorf("error parsing included templates set for %s: %w", page, err)
		}

		cache[name] = tSet
	}

	return cache, nil
}

func (s *Service) render(w http.ResponseWriter, template string, data any, headers http.Header, status int) error {

	// Check whether that template exists in the cache
	tmpl, ok := s.Template[template]
	if !ok {
		return fmt.Errorf("template %s is not available in the cache", template)
	}

	var b bytes.Buffer
	if err := tmpl.ExecuteTemplate(&b, "base", data); err != nil {
		return fmt.Errorf("error executing template %s: %w", template, err)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	for key, values := range headers {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(status)

	if _, err := w.Write(b.Bytes()); err != nil {
		return fmt.Errorf("error rendering %s: %w", template, err)
	}

	return nil
}

func Mul(i1 int, f2 float64) float64 {
	return float64(i1) * f2
}

func Add(i1, i2 int) int {
	return i1 + i2
}

func formatMoney(amount float64) string {
	p := message.NewPrinter(language.English) // Use English locale for comma separators
	return p.Sprintf("%.2f", amount)          // Format with two decimal places
}

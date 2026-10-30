package handlers

import (
	"bytes"
	"forum/templates"
	"html/template"
	"log"
	"net/http"
)

func parsePage(name string) (*template.Template, error) {
	return template.ParseFS(templates.Files, name, "layout.html")
}
func executeTemplate(w http.ResponseWriter, tmpl *template.Template, data interface{}) error {
	var body bytes.Buffer
	if err := tmpl.Execute(&body, data); err != nil {
		log.Printf("template=%s error=%v", tmpl.Name(), err)
		return err
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, err := w.Write(body.Bytes())
	return err
}

func renderTemplateBytes(tmpl *template.Template, data interface{}) ([]byte, error) {
	var b bytes.Buffer
	err := tmpl.Execute(&b, data)
	return b.Bytes(), err
}

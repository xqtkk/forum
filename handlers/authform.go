package handlers

import (
	"bytes"
	"net/http"
)

type authForm struct {
	Check, Email, Username       string
	GoogleEnabled, GitHubEnabled bool
}

func renderAuth(w http.ResponseWriter, name string, status int, data authForm) {
	data.GoogleEnabled = oauthConfigured("google")
	data.GitHubEnabled = oauthConfigured("github")
	tmpl, err := parsePage(name)
	var body bytes.Buffer
	if err != nil {
		ErrorHandler(w, "Template loading error", 500)
		return
	}
	if err = tmpl.Execute(&body, data); err != nil {
		ErrorHandler(w, "Template rendering error", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body.Bytes())
}

package handlers

import (
	"bytes"
	"log"
	"net/http"
)

type Errors struct {
	Message string
	Code    int
}

func ErrorHandler(w http.ResponseWriter, message string, code int) {
	tmpl, err := parsePage("error.html")
	var body bytes.Buffer
	if err == nil {
		err = tmpl.Execute(&body, Errors{message, code})
	}
	if err != nil {
		log.Printf("error-page: %v", err)
		http.Error(w, message, code)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write(body.Bytes())
}

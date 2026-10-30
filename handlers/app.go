package handlers

import (
	"context"
	"database/sql"
	"forum/database"
	"log"
	"net/http"
	"time"
)

type App struct{ DB *sql.DB }
type appKey struct{}

func (a *App) Handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), appKey{}, a)))
	})
}
func requestDB(r *http.Request) (*sql.DB, func(), error) {
	if r != nil {
		if app, ok := r.Context().Value(appKey{}).(*App); ok {
			return app.DB, func() {}, nil
		}
	}
	db, err := database.OpenDB()
	return db, func() {
		if db != nil {
			db.Close()
		}
	}, err
}

type requestWriter struct {
	http.ResponseWriter
	status int
}

func (w *requestWriter) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
		w.ResponseWriter.WriteHeader(status)
	}
}
func (w *requestWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(b)
}
func RequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		out := &requestWriter{ResponseWriter: w}
		defer func() {
			if failure := recover(); failure != nil {
				log.Printf("panic method=%s path=%s error=%v", r.Method, r.URL.Path, failure)
				if out.status == 0 {
					http.Error(out, "Internal server error", 500)
				}
			}
			log.Printf("request method=%s path=%s status=%d elapsed=%s", r.Method, r.URL.Path, out.status, time.Since(started))
		}()
		next.ServeHTTP(out, r)
	})
}

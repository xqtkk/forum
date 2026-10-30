package main

import (
	"context"
	"forum/database"
	"forum/handlers"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAllAuthenticatedGetRoutesRenderThroughRealMux(t *testing.T) {
	t.Setenv("FORUM_DB_PATH", filepath.Join(t.TempDir(), "forum.db"))
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Exec("INSERT INTO users(id,email,username,password,role) VALUES(1,'admin@example.test','admin','!test','admin');INSERT INTO sessions(session_id,user_id,expires_at) VALUES('test-session',1,datetime('now','+1 hour'));INSERT INTO posts(id,title,content,author_id) VALUES(1,'Post','Content',1);INSERT INTO notifications(user_id,notification_type) VALUES(1,'Test notification')"); err != nil {
		t.Fatal(err)
	}
	app := &handlers.App{DB: db}
	mux := app.Handler(handlers.ProtectRequests(newMux()))
	for _, path := range []string{"/", "/register", "/login", "/notifications", "/notifications/read", "/profile", "/profile?id=1&tab=comments", "/post?id=1", "/create-post", "/edit-post?id=1", "/my-posts", "/liked-posts", "/activity-page", "/reports", "/categories", "/users", "/requests", "/gomoder", "/forgot-password", "/reset-password", "/verify-email"} {
		t.Run(path, func(t *testing.T) {
			r := httptest.NewRequest("GET", "https://localhost:4000"+path, nil)
			r.Header.Set("Cookie", "session_id=test-session")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			body := w.Body.String()
			if w.Code != 200 || strings.Contains(body, "Failed to render template") || !strings.Contains(body, "site-header") {
				t.Fatalf("route failed: %d %s", w.Code, body)
			}
		})
	}
}

func TestServerGracefulShutdown(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	listener.Close()
	dir := t.TempDir()
	t.Setenv("FORUM_DB_PATH", filepath.Join(dir, "forum.db"))
	t.Setenv("FORUM_BACKUP_DIR", filepath.Join(dir, "backups"))
	t.Setenv("FORUM_ADDR", addr)
	t.Setenv("FORUM_HTTP", "1")
	t.Setenv("PUBLIC_BASE_URL", "http://"+addr)
	t.Setenv("FORUM_ENV", "")
	t.Setenv("FORUM_DEV_MAIL", "1")
	t.Setenv("TRUSTED_PROXY_CIDRS", "")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- run(ctx) }()
	client := &http.Client{Timeout: time.Second}
	ready := false
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); {
		response, err := client.Get("http://" + addr + "/")
		if err == nil {
			response.Body.Close()
			ready = response.StatusCode == 200
			if ready {
				break
			}
		}
		select {
		case err := <-finished:
			t.Fatalf("server exited before listening: %v", err)
		case <-time.After(20 * time.Millisecond):
		}
	}
	if !ready {
		t.Fatal("server did not become ready")
	}
	cancel()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down")
	}
}

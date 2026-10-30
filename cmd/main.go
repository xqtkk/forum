package main

import (
	"context"
	"errors"
	"forum/database"
	"forum/handlers"
	"forum/serverconfig"
	"golang.org/x/time/rate"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

type clientLimit struct {
	limiter *rate.Limiter
	seen    time.Time
}

var clientLimits = struct {
	sync.Mutex
	entries map[string]clientLimit
}{entries: make(map[string]clientLimit)}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		log.Fatal(err)
	}
}
func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("/static/", noCacheAssets(http.StripPrefix("/static/", http.FileServer(http.Dir("./static")))))
	mux.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("uploads"))))
	routes := map[string]http.HandlerFunc{
		"/": handlers.HomeHandler, "/register": handlers.RegisterHandler, "/login": handlers.LoginHandler, "/logout": handlers.LogoutHandler,
		"/create-post": handlers.CreatePostHandler, "/edit-post": handlers.EditPostHandler, "/post": handlers.PostHandler, "/comment": handlers.CommentHandler,
		"/my-posts": handlers.UserPostHandler, "/liked-posts": handlers.LikePostHandler, "/notifications": handlers.NotificationsHandler, "/notifications/read": handlers.ReadNotificationsHandler, "/notifications/count": handlers.NotificationCountHandler,
		"/activity-page": handlers.GetUserActivity, "/profile": handlers.ProfileHandler,
		"/gomoder": handlers.ShowPromotionFormHandler, "/submit-promotion-request": handlers.SubmitPromotionRequestHandler, "/approve-request": handlers.ApproveRequestHandler, "/deny-request": handlers.DenyRequestHandler, "/requests": handlers.AdminPromotionRequestsHandler,
		"/reports": handlers.ReportsHandler, "/categories": handlers.CategoryHandler, "/users": handlers.UserListHandler,
		"/google/login": handlers.GoogleLogin, "/google/callback": handlers.GoogleCallback, "/callback": handlers.GoogleCallback, "/github/login": handlers.GitHubLogin, "/github/callback": handlers.GitHubCallback,
		"/account/connect": handlers.ConnectOAuthHandler, "/account/email": handlers.RequestVerificationHandler, "/forgot-password": handlers.ForgotPasswordHandler, "/reset-password": handlers.ResetPasswordHandler, "/verify-email": handlers.VerifyEmailHandler,
	}
	for path, handler := range routes {
		mux.Handle(path, RateLimitMiddleware(handler))
	}
	return mux
}
func run(ctx context.Context) error {
	addr := os.Getenv("FORUM_ADDR")
	if addr == "" {
		addr = "127.0.0.1:4000"
	}
	tlsConfig, err := serverconfig.Configure(addr)
	if err != nil {
		return err
	}
	db, err := database.InitDB()
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err = db.Exec("INSERT OR IGNORE INTO categories(name) VALUES('General'),('Technology')"); err != nil {
		return err
	}
	if err = serverconfig.DailyBackup(db); err != nil {
		return err
	}
	app := &handlers.App{DB: db}
	server := &http.Server{Addr: addr, Handler: handlers.RequestLog(app.Handler(handlers.ProtectRequests(newMux()))), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 45 * time.Second, IdleTimeout: 60 * time.Second, TLSConfig: tlsConfig}
	errorsCh := make(chan error, 1)
	go func() {
		log.Printf("Forum listening on %s", os.Getenv("PUBLIC_BASE_URL"))
		if tlsConfig == nil {
			errorsCh <- server.ListenAndServe()
		} else {
			errorsCh <- server.ListenAndServeTLS("", "")
		}
	}()
	jobCtx, cancelJobs := context.WithCancel(ctx)
	jobsDone := make(chan struct{})
	go func() { defer close(jobsDone); serverconfig.Maintain(jobCtx, db) }()
	defer func() { cancelJobs(); <-jobsDone }()
	select {
	case err = <-errorsCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		cancelJobs()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		log.Print("Finishing active requests...")
		if err = server.Shutdown(shutdown); err != nil {
			server.Close()
			return err
		}
		return nil
	}
}
func RateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if (r.Method == http.MethodGet || r.Method == http.MethodHead) && r.URL.Path != "/google/login" && r.URL.Path != "/github/login" {
			next.ServeHTTP(w, r)
			return
		}
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		now := time.Now()
		clientLimits.Lock()
		for key, value := range clientLimits.entries {
			if now.Sub(value.seen) > 10*time.Minute {
				delete(clientLimits.entries, key)
			}
		}
		entry, ok := clientLimits.entries[host]
		if !ok {
			if len(clientLimits.entries) >= 10000 {
				clientLimits.Unlock()
				w.Header().Set("Retry-After", "60")
				handlers.ErrorHandler(w, "Too many requests", 429)
				return
			}
			entry = clientLimit{limiter: rate.NewLimiter(rate.Every(500*time.Millisecond), 30)}
		}
		entry.seen = now
		clientLimits.entries[host] = entry
		allowed := entry.limiter.Allow()
		clientLimits.Unlock()
		if !allowed {
			w.Header().Set("Retry-After", "2")
			handlers.ErrorHandler(w, "Too many requests. Please wait a moment.", 429)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func noCacheAssets(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}

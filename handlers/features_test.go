package handlers

import (
	"fmt"
	"forum/database"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNotificationsHandlerRendersLayoutAndDeletedReferences(t *testing.T) {
	testDB(t)
	owner := addUser(t, "notice@example.test", "notice_user", "user")
	cookie := loginCookie(t, "notice@example.test")
	db, _ := database.OpenDB()
	defer db.Close()
	if _, err := db.Exec("INSERT INTO notifications(user_id,notification_type,comment_content) VALUES(?,'You received a comment','Saved snapshot')", owner); err != nil {
		t.Fatal(err)
	}
	for _, handler := range []http.HandlerFunc{NotificationsHandler, ReadNotificationsHandler} {
		r := httptest.NewRequest("GET", "https://localhost:4000/notifications", nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		handler(w, r)
		if w.Code != 200 || strings.Contains(w.Body.String(), "Failed to render template") || !strings.Contains(w.Body.String(), "site-header") {
			t.Fatalf("notification template failed: %d %s", w.Code, w.Body.String())
		}
	}
}

func TestReactionsReturnJSONWithoutNavigation(t *testing.T) {
	testDB(t)
	owner := addUser(t, "react@example.test", "react_user", "user")
	cookie := loginCookie(t, "react@example.test")
	db, _ := database.OpenDB()
	defer db.Close()
	if _, err := db.Exec("INSERT INTO posts(id,title,content,author_id) VALUES(1,'Title','Content',?);", owner); err != nil {
		t.Fatal(err)
	}
	if err := database.AddComment(db, 1, owner, "Comment"); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"post", "comment"} {
		for _, action := range []string{"like", "dislike", "dislike"} {
			r := httptest.NewRequest("POST", "https://localhost:4000/"+kind+"?id=1&action="+action, strings.NewReader(""))
			r.AddCookie(cookie)
			r.Header.Set("Accept", "application/json")
			w := httptest.NewRecorder()
			handler := http.HandlerFunc(PostHandler)
			if kind == "comment" {
				handler = CommentHandler
			}
			handler(w, r)
			if w.Code != 200 || w.Header().Get("Location") != "" || !strings.Contains(w.Header().Get("Content-Type"), "application/json") {
				t.Fatalf("reaction navigates: %d %s", w.Code, w.Body.String())
			}
		}
	}
}

func TestProfilesPublicPrivacyAndBioOwnership(t *testing.T) {
	testDB(t)
	owner := addUser(t, "private@example.test", "profile_owner", "user")
	addUser(t, "visitor@example.test", "visitor", "user")
	ownerCookie := loginCookie(t, "private@example.test")
	other := loginCookie(t, "visitor@example.test")
	w := submit(ProfileHandler, "/profile", url.Values{"bio": {"My public bio <script>unsafe()</script>"}}, ownerCookie)
	if w.Code != 303 {
		t.Fatalf("save bio %d", w.Code)
	}
	w = submit(ProfileHandler, fmt.Sprintf("/profile?id=%d", owner), url.Values{"bio": {"Stolen"}}, other)
	if w.Code != 403 {
		t.Fatal("other user edited bio")
	}
	r := httptest.NewRequest("GET", fmt.Sprintf("https://localhost:4000/profile?id=%d", owner), nil)
	r.AddCookie(other)
	w = httptest.NewRecorder()
	ProfileHandler(w, r)
	body := w.Body.String()
	if w.Code != 200 || strings.Contains(body, "private@example.test") || strings.Contains(body, "<script>unsafe()") || !strings.Contains(body, "My public bio") {
		t.Fatalf("profile privacy/escaping failed %d %s", w.Code, body)
	}
}

func TestFeedPaginationPreservesCategory(t *testing.T) {
	testDB(t)
	owner := addUser(t, "page@example.test", "page_user", "user")
	db, _ := database.OpenDB()
	defer db.Close()
	if _, err := db.Exec("INSERT INTO categories(id,name) VALUES(1,'General')"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 21; i++ {
		if _, err := db.Exec("INSERT INTO posts(title,content,author_id,category_id) VALUES(?,'Content',?,1)", fmt.Sprintf("Post %02d", i), owner); err != nil {
			t.Fatal(err)
		}
	}
	r := httptest.NewRequest("GET", "https://localhost:4000/?category_id=1&page=1", nil)
	w := httptest.NewRecorder()
	HomeHandler(w, r)
	if w.Code != 200 || strings.Count(w.Body.String(), `class="discussion-card"`) != 20 || !strings.Contains(w.Body.String(), "category_id=1&amp;page=2") {
		t.Fatalf("page one incorrect: %d", w.Code)
	}
	r = httptest.NewRequest("GET", "https://localhost:4000/?category_id=1&page=2", nil)
	w = httptest.NewRecorder()
	HomeHandler(w, r)
	if strings.Count(w.Body.String(), `class="discussion-card"`) != 1 {
		t.Fatal("page two incorrect")
	}
}

func TestPasswordResetSingleUseRevokesSessionsAndLock(t *testing.T) {
	testDB(t)
	owner := addUser(t, "reset@example.test", "reset_user", "user")
	cookie := loginCookie(t, "reset@example.test")
	var message string
	old := sendAccountMail
	sendAccountMail = func(to, subject, body string) error { message = body; return nil }
	t.Cleanup(func() { sendAccountMail = old })
	db, _ := database.OpenDB()
	defer db.Close()
	for i := 0; i < 5; i++ {
		if err := recordLoginFailure(db, "reset@example.test"); err != nil {
			t.Fatal(err)
		}
	}
	if err := issueAccountToken(db, owner, "reset", "reset@example.test"); err != nil {
		t.Fatal(err)
	}
	token := strings.Split(strings.Split(message, "?token=")[1], "\n")[0]
	values := url.Values{"token": {token}, "password": {"Changedpass2!"}}
	w := submit(ResetPasswordHandler, "/reset-password", values, nil)
	if w.Code != 200 {
		t.Fatalf("reset %d %s", w.Code, w.Body.String())
	}
	if _, valid := lookupSession(cookie.Value); valid {
		t.Fatal("reset did not revoke existing sessions")
	}
	if blocked, err := loginBlocked(db, "reset@example.test"); err != nil || blocked {
		t.Fatal("reset did not clear login lock")
	}
	w = submit(ResetPasswordHandler, "/reset-password", values, nil)
	if w.Code != 400 {
		t.Fatal("reset token reused")
	}
	w = submit(LoginHandler, "/login", url.Values{"email": {"reset@example.test"}, "password": {"Changedpass2!"}}, nil)
	if w.Code != 303 {
		t.Fatalf("new password failed: %d", w.Code)
	}
}

func TestVerificationDoesNotConsumeOnGet(t *testing.T) {
	testDB(t)
	owner := addUser(t, "verify@example.test", "verify_user", "user")
	db, _ := database.OpenDB()
	defer db.Close()
	db.Exec("UPDATE users SET email_verified=0 WHERE id=?", owner)
	var message string
	old := sendAccountMail
	sendAccountMail = func(to, subject, body string) error { message = body; return nil }
	t.Cleanup(func() { sendAccountMail = old })
	if err := issueAccountToken(db, owner, "verify", "verify@example.test"); err != nil {
		t.Fatal(err)
	}
	token := strings.Split(strings.Split(message, "?token=")[1], "\n")[0]
	w := httptest.NewRecorder()
	VerifyEmailHandler(w, httptest.NewRequest("GET", "https://localhost:4000/verify-email?token="+token, nil))
	var verified bool
	db.QueryRow("SELECT email_verified FROM users WHERE id=?", owner).Scan(&verified)
	if verified {
		t.Fatal("email scanner GET consumed token")
	}
	w = submit(VerifyEmailHandler, "/verify-email", url.Values{"token": {token}}, nil)
	if w.Code != 200 {
		t.Fatalf("verify %d", w.Code)
	}
	db.QueryRow("SELECT email_verified FROM users WHERE id=?", owner).Scan(&verified)
	if !verified {
		t.Fatal("email not verified")
	}
}

func TestExplicitOAuthLinkAndReplayProtection(t *testing.T) {
	testDB(t)
	configureOAuth(t, true)
	owner := addUser(t, "link@example.test", "link_user", "user")
	cookie := loginCookie(t, "link@example.test")
	w := submit(ConnectOAuthHandler, "/account/connect?provider=google", url.Values{}, cookie)
	if w.Code != 302 {
		t.Fatalf("link start %d", w.Code)
	}
	destination, _ := url.Parse(w.Header().Get("Location"))
	state := destination.Query().Get("state")
	r := httptest.NewRequest("GET", "https://localhost:4000/google/callback?code=test&state="+state, nil)
	r.AddCookie(cookie)
	r.AddCookie(w.Result().Cookies()[0])
	response := httptest.NewRecorder()
	GoogleCallback(response, r)
	if response.Code != 303 || response.Header().Get("Location") != "/profile?notice=linked" {
		t.Fatalf("link callback %d %s", response.Code, response.Body.String())
	}
	db, _ := database.OpenDB()
	defer db.Close()
	var linked int
	db.QueryRow("SELECT user_id FROM oauth_accounts WHERE provider='google'").Scan(&linked)
	if linked != owner {
		t.Fatal("provider linked to wrong account")
	}
	replay := httptest.NewRecorder()
	GoogleCallback(replay, r)
	if replay.Code != 400 {
		t.Fatal("OAuth flow replay accepted")
	}
}

func TestAppReusesPoolWithoutClosingIt(t *testing.T) {
	testDB(t)
	db, err := database.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	app := &App{DB: db}
	handler := app.Handler(http.HandlerFunc(HomeHandler))
	for i := 0; i < 4; i++ {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("GET", "https://localhost:4000/", nil))
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	if err = db.Ping(); err != nil {
		t.Fatal("request closed shared pool")
	}
}

func TestForwardingHeadersOnlyTrustedFromConfiguredProxy(t *testing.T) {
	t.Setenv("TRUSTED_PROXY_CIDRS", "127.0.0.1/32")
	r := httptest.NewRequest("POST", "http://forum.example.test/profile", nil)
	r.Header.Set("Origin", "https://forum.example.test")
	r.Header.Set("X-Forwarded-Proto", "https")
	r.RemoteAddr = "203.0.113.5:5000"
	handler := ProtectRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("untrusted client spoofed forwarded scheme")
	}
	r.RemoteAddr = "127.0.0.1:5000"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatalf("trusted HTTPS proxy rejected: %d", w.Code)
	}
	if !sessionCookie(r, "token", 3600).Secure {
		t.Fatal("proxy HTTPS cookie is not Secure")
	}
}

func TestProductionEmailVerificationAndPerAccountLimit(t *testing.T) {
	testDB(t)
	t.Setenv("REQUIRE_EMAIL_VERIFICATION", "1")
	owner := addUser(t, "unverified@example.test", "unverified_user", "user")
	w := submit(LoginHandler, "/login", url.Values{"email": {"unverified@example.test"}, "password": {"Goodpass1!"}}, nil)
	if w.Code != 403 {
		t.Fatal("unverified email can sign in when verification is required")
	}
	db, _ := database.OpenDB()
	defer db.Close()
	db.Exec("UPDATE users SET email_verified=1 WHERE id=?", owner)
	for i := 0; i < 5; i++ {
		w = submit(LoginHandler, "/login", url.Values{"email": {"unverified@example.test"}, "password": {"Wrongpass!"}}, nil)
		if w.Code != 401 {
			t.Fatalf("attempt %d: %d", i, w.Code)
		}
	}
	w = submit(LoginHandler, "/login", url.Values{"email": {"unverified@example.test"}, "password": {"Goodpass1!"}}, nil)
	if w.Code != 429 {
		t.Fatal("failed login limit not enforced")
	}
}

func TestDevelopmentMailIsLocalAndNotExposedInResponse(t *testing.T) {
	testDB(t)
	addUser(t, "mail@example.test", "mail_user", "user")
	dir := t.TempDir()
	t.Setenv("SMTP_HOST", "")
	t.Setenv("FORUM_DEV_MAIL", "1")
	t.Setenv("FORUM_MAIL_DIR", dir)
	w := submit(ForgotPasswordHandler, "/forgot-password", url.Values{"email": {"mail@example.test"}}, nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), "?token=") {
		t.Fatal("reset link leaked in HTTP response")
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != 1 {
		t.Fatal("development mail not saved")
	}
	mail, err := os.ReadFile(filepath.Join(dir, files[0].Name()))
	if err != nil || !strings.Contains(string(mail), "/reset-password?token=") {
		t.Fatal("outbox missing reset link")
	}
}

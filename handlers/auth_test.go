package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"forum/database"
	"forum/templates"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func testDB(t *testing.T) {
	t.Helper()
	t.Setenv("FORUM_DB_PATH", filepath.Join(t.TempDir(), "test.db"))
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
}

func submit(handler http.HandlerFunc, path string, values url.Values, cookie *http.Cookie) *httptest.ResponseRecorder {
	r := httptest.NewRequest("POST", "https://localhost:4000"+path, strings.NewReader(values.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Origin", "https://localhost:4000")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	handler(w, r)
	return w
}

func addUser(t *testing.T, email, name, role string) int {
	t.Helper()
	db, err := database.OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = database.CreateUser(db, email, name, "Goodpass1!", role); err != nil {
		t.Fatal(err)
	}
	user, err := database.GetUserByEmail(db, email)
	if err != nil {
		t.Fatal(err)
	}
	return user.ID
}

func loginCookie(t *testing.T, email string) *http.Cookie {
	t.Helper()
	w := submit(LoginHandler, "/login", url.Values{"email": {email}, "password": {"Goodpass1!"}}, nil)
	if w.Code != 303 {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == "session_id" {
			return cookie
		}
	}
	t.Fatal("missing session cookie")
	return nil
}

func TestRegistrationRetainsNonSecretFields(t *testing.T) {
	testDB(t)
	for _, password := range []string{"weak", "longenough1!", "Longenough!", "Longenough1", strings.Repeat("A", 73) + "1!"} {
		w := submit(RegisterHandler, "/register", url.Values{"username": {"  person_1  "}, "email": {" PERSON@example.test "}, "password": {password}}, nil)
		if w.Code != 422 {
			t.Fatalf("expected 422: %d", w.Code)
		}
		body := w.Body.String()
		for _, value := range []string{`value="person_1"`, `value="person@example.test"`, `role="alert"`} {
			if !strings.Contains(body, value) {
				t.Errorf("missing %s", value)
			}
		}
		if strings.Contains(body, `value="`+password+`"`) {
			t.Fatal("password echoed into form")
		}
	}
	// A different browser must never inherit another visitor's validation error.
	w := httptest.NewRecorder()
	RegisterHandler(w, httptest.NewRequest("GET", "https://localhost:4000/register", nil))
	if strings.Contains(w.Body.String(), `role="alert"`) {
		t.Fatal("error leaked across requests")
	}
}

func TestRegistrationDuplicateAndEscaping(t *testing.T) {
	testDB(t)
	addUser(t, "taken@example.test", "taken_name", "user")
	for _, values := range []url.Values{
		{"email": {"TAKEN@example.test"}, "username": {"other_name"}, "password": {"Goodpass1!"}},
		{"email": {"other@example.test"}, "username": {"taken_name"}, "password": {"Goodpass1!"}},
		{"email": {"safe@example.test"}, "username": {`"><script>alert(1)</script>`}, "password": {"weak"}},
	} {
		w := submit(RegisterHandler, "/register", values, nil)
		if w.Code != 422 {
			t.Fatalf("expected validation error: %d", w.Code)
		}
		if strings.Contains(w.Body.String(), `<script>alert(1)</script>`) {
			t.Fatal("unescaped form input")
		}
	}
	w := submit(RegisterHandler, "/register", url.Values{"email": {"new@example.test"}, "username": {"new_user"}, "password": {"Goodpass1!"}}, nil)
	if w.Code != 303 || w.Header().Get("Location") != "/login?registered=1" {
		t.Fatalf("registration failed: %d", w.Code)
	}
}

func TestSessionsRotateExpireAndLogout(t *testing.T) {
	testDB(t)
	userID := addUser(t, "user@example.test", "user_name", "user")
	cookie := loginCookie(t, "USER@example.test")
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal("unsafe cookie attributes")
	}
	if user, ok := lookupSession(cookie.Value); !ok || user.ID != userID {
		t.Fatal("missing persisted session")
	}
	w := submit(LoginHandler, "/login", url.Values{"email": {"user@example.test"}, "password": {"Goodpass1!"}}, cookie)
	rotated := w.Result().Cookies()[0]
	if rotated.Value == cookie.Value {
		t.Fatal("session was not rotated")
	}
	if _, ok := lookupSession(cookie.Value); ok {
		t.Fatal("old session still valid")
	}
	db, _ := database.OpenDB()
	_, err := db.Exec("UPDATE sessions SET expires_at='2000-01-01 00:00:00' WHERE session_id=?", rotated.Value)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := lookupSession(rotated.Value); ok {
		t.Fatal("expired session accepted")
	}
	cookie = loginCookie(t, "user@example.test")
	w = submit(LogoutHandler, "/logout", url.Values{}, cookie)
	if w.Code != 303 {
		t.Fatalf("logout %d", w.Code)
	}
	if _, ok := lookupSession(cookie.Value); ok {
		t.Fatal("session survived logout")
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func configureOAuth(t *testing.T, verified bool) {
	t.Helper()
	for _, provider := range []string{"GOOGLE", "GITHUB"} {
		t.Setenv(provider+"_CLIENT_ID", "test-id")
		t.Setenv(provider+"_CLIENT_SECRET", "test-secret")
	}
	t.Setenv("PUBLIC_BASE_URL", "https://localhost:4000")
	old := oauthHTTPClient
	t.Cleanup(func() { oauthHTTPClient = old })
	oauthHTTPClient = &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		var body string
		switch r.URL.Host + r.URL.Path {
		case "oauth2.googleapis.com/token", "github.com/login/oauth/access_token":
			if err := r.ParseForm(); err != nil {
				return nil, err
			}
			if r.Form.Get("code_verifier") == "" {
				return nil, fmt.Errorf("missing PKCE verifier")
			}
			body = `{"access_token":"test-token","token_type":"Bearer","expires_in":3600}`
		case "www.googleapis.com/oauth2/v3/userinfo":
			body = fmt.Sprintf(`{"sub":"google-123","name":"Shared Name","email":"google@example.test","email_verified":%t}`, verified)
		case "api.github.com/user":
			body = `{"id":456,"login":"shared_name","email":null}`
		case "api.github.com/user/emails":
			body = fmt.Sprintf(`[{"email":"unverified@example.test","primary":false,"verified":false},{"email":"github@example.test","primary":true,"verified":%t}]`, verified)
		default:
			return nil, fmt.Errorf("unexpected endpoint %s", r.URL)
		}
		if strings.Contains(r.URL.Path, "/user") && r.Header.Get("Authorization") != "Bearer test-token" {
			return nil, fmt.Errorf("missing bearer token")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
	})}
}

func oauthRoundTrip(t *testing.T, provider string) *httptest.ResponseRecorder {
	t.Helper()
	start := httptest.NewRecorder()
	beginOAuth(start, httptest.NewRequest("GET", "https://localhost:4000/"+provider+"/login", nil), provider)
	if start.Code != 302 {
		t.Fatalf("oauth start: %d %s", start.Code, start.Body.String())
	}
	destination, _ := url.Parse(start.Header().Get("Location"))
	state := destination.Query().Get("state")
	if state == "random" || len(state) < 32 || destination.Query().Get("code_challenge_method") != "S256" {
		t.Fatal("missing state/PKCE")
	}
	if destination.Query().Get("redirect_uri") != "https://localhost:4000/"+provider+"/callback" {
		t.Fatal("incorrect callback")
	}
	r := httptest.NewRequest("GET", "https://localhost:4000/"+provider+"/callback?code=test-code&state="+state, nil)
	r.AddCookie(start.Result().Cookies()[0])
	done := httptest.NewRecorder()
	finishOAuth(done, r, provider)
	return done
}

func TestOAuthProvidersCreateAndReuseIdentity(t *testing.T) {
	for _, provider := range []string{"google", "github"} {
		t.Run(provider, func(t *testing.T) {
			testDB(t)
			configureOAuth(t, true)
			for attempt := 0; attempt < 2; attempt++ {
				w := oauthRoundTrip(t, provider)
				if w.Code != 303 {
					t.Fatalf("oauth callback: %d %s", w.Code, w.Body.String())
				}
			}
			db, _ := database.OpenDB()
			defer db.Close()
			var count int
			var password, email string
			if err := db.QueryRow("SELECT COUNT(*),password,email FROM users").Scan(&count, &password, &email); err != nil {
				t.Fatal(err)
			}
			if count != 1 || password != "!oauth" || email != provider+"@example.test" {
				t.Fatalf("invalid social account: %d %s %s", count, password, email)
			}
			if database.ComparePassword(password, "defaultpassword") == nil {
				t.Fatal("social account has shared password")
			}
		})
	}
}

func TestOAuthRejectsUnverifiedEmailAndCollision(t *testing.T) {
	for _, provider := range []string{"google", "github"} {
		t.Run(provider, func(t *testing.T) {
			testDB(t)
			configureOAuth(t, false)
			if w := oauthRoundTrip(t, provider); w.Code != 502 {
				t.Fatalf("accepted unverified email: %d", w.Code)
			}
			configureOAuth(t, true)
			addUser(t, provider+"@example.test", "existing", "user")
			if w := oauthRoundTrip(t, provider); w.Code != 409 {
				t.Fatalf("silently linked existing account: %d", w.Code)
			}
		})
	}
}

func TestOAuthStateAndMissingConfiguration(t *testing.T) {
	testDB(t)
	configureOAuth(t, true)
	for _, query := range []string{"?code=stolen", "?code=stolen&state=random", "?error=access_denied"} {
		w := httptest.NewRecorder()
		GoogleCallback(w, httptest.NewRequest("GET", "https://localhost:4000/google/callback"+query, nil))
		if w.Code != 400 {
			t.Fatalf("invalid state accepted: %d", w.Code)
		}
	}
	t.Setenv("GOOGLE_CLIENT_ID", "")
	w := httptest.NewRecorder()
	GoogleLogin(w, httptest.NewRequest("GET", "https://localhost:4000/google/login", nil))
	if w.Code != 503 || w.Header().Get("Location") != "" {
		t.Fatal("unconfigured provider redirects to invalid external login")
	}
}

func TestIdentityUsernameCollision(t *testing.T) {
	testDB(t)
	first, err := resolveIdentity(providerIdentity{"google", "one", "first@example.test", "same_name"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := resolveIdentity(providerIdentity{"github", "two", "second@example.test", "same_name"})
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("identities merged")
	}
}

func TestResourceOwnership(t *testing.T) {
	testDB(t)
	owner := addUser(t, "owner@example.test", "owner", "user")
	addUser(t, "other@example.test", "other", "user")
	cookie := loginCookie(t, "other@example.test")
	db, _ := database.OpenDB()
	result, err := db.Exec("INSERT INTO posts(title,content,author_id) VALUES('Post','Content',?)", owner)
	if err != nil {
		t.Fatal(err)
	}
	postID, _ := result.LastInsertId()
	result, err = db.Exec("INSERT INTO comments(post_id,user_id,content) VALUES(?,?,'Original')", postID, owner)
	if err != nil {
		t.Fatal(err)
	}
	commentID, _ := result.LastInsertId()
	db.Close()
	for _, path := range []string{fmt.Sprintf("/post?id=%d&action=delete", postID), fmt.Sprintf("/comment?id=%d&action=delete", commentID), fmt.Sprintf("/comment?id=%d&action=update", commentID)} {
		handler := http.HandlerFunc(PostHandler)
		if strings.HasPrefix(path, "/comment") {
			handler = CommentHandler
		}
		w := submit(handler, path, url.Values{"content": {"Changed"}}, cookie)
		if w.Code != 403 {
			t.Fatalf("%s: expected 403 got %d", path, w.Code)
		}
	}
	r := httptest.NewRequest("GET", fmt.Sprintf("https://localhost:4000/edit-post?id=%d", postID), nil)
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	EditPostHandler(w, r)
	if w.Code != 403 {
		t.Fatalf("edit ownership not enforced: %d", w.Code)
	}
}

func TestCrossOriginWrites(t *testing.T) {
	protected := ProtectRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	for _, origin := range []string{"https://evil.test", "null", ""} {
		r := httptest.NewRequest("POST", "https://localhost:4000/register", nil)
		r.Header.Set("Origin", origin)
		w := httptest.NewRecorder()
		protected.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("accepted origin %q", origin)
		}
	}
	r := httptest.NewRequest("POST", "https://localhost:4000/register", nil)
	r.Header.Set("Origin", "https://localhost:4000")
	w := httptest.NewRecorder()
	protected.ServeHTTP(w, r)
	if w.Code != 204 {
		t.Fatalf("same-origin form rejected: %d", w.Code)
	}
}

func TestAllPageTemplatesRender(t *testing.T) {
	files, err := templates.Files.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".html") || file.Name() == "layout.html" {
			continue
		}
		t.Run(file.Name(), func(t *testing.T) {
			tmpl, err := parsePage(file.Name())
			if err != nil {
				t.Fatal(err)
			}
			data := map[string]interface{}{"Username": "A", "Admin": true, "LoggedIn": true, "SelectedCategory": 0, "Posts": []database.Post{}, "Post": database.Post{}, "SessionUserID": 1}
			var body bytes.Buffer
			if err = tmpl.Execute(&body, data); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(body.String(), "/static/app.css") {
				t.Fatal("page does not use shared styling")
			}
		})
	}
}

func TestProviderNonSuccessStatus(t *testing.T) {
	client := &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 500, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})}
	var value json.RawMessage
	if getProviderJSON(context.Background(), client, "https://api.github.com/user", &value) == nil {
		t.Fatal("accepted failed API response")
	}
}

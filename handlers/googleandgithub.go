package handlers

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var oauthHTTPClient = &http.Client{Timeout: 10 * time.Second}

func oauthConfigured(provider string) bool {
	prefix := strings.ToUpper(provider)
	return strings.TrimSpace(os.Getenv(prefix+"_CLIENT_ID")) != "" && strings.TrimSpace(os.Getenv(prefix+"_CLIENT_SECRET")) != ""
}

func oauthConfig(provider string) (oauth2.Config, error) {
	prefix := strings.ToUpper(provider)
	if !oauthConfigured(provider) {
		return oauth2.Config{}, fmt.Errorf("%s sign-in is not configured yet. Use email registration or ask the site owner to configure OAuth.", prefix)
	}
	base := strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/")
	if base == "" {
		base = "https://localhost:4000"
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return oauth2.Config{}, errors.New("Invalid PUBLIC_BASE_URL configuration")
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && (u.Hostname() == "localhost" || u.Hostname() == "127.0.0.1")) {
		return oauth2.Config{}, errors.New("OAuth requires HTTPS except on localhost")
	}
	cfg := oauth2.Config{ClientID: os.Getenv(prefix + "_CLIENT_ID"), ClientSecret: os.Getenv(prefix + "_CLIENT_SECRET"), RedirectURL: base + "/" + provider + "/callback"}
	switch provider {
	case "google":
		cfg.Endpoint = google.Endpoint
		cfg.Scopes = []string{"openid", "profile", "email"}
	case "github":
		cfg.Endpoint = oauth2.Endpoint{AuthURL: "https://github.com/login/oauth/authorize", TokenURL: "https://github.com/login/oauth/access_token", AuthStyle: oauth2.AuthStyleInParams}
		cfg.Scopes = []string{"read:user", "user:email"}
	default:
		return oauth2.Config{}, errors.New("Unknown identity provider")
	}
	return cfg, nil
}

func GoogleLogin(w http.ResponseWriter, r *http.Request)    { beginOAuth(w, r, "google") }
func GitHubLogin(w http.ResponseWriter, r *http.Request)    { beginOAuth(w, r, "github") }
func GoogleCallback(w http.ResponseWriter, r *http.Request) { finishOAuth(w, r, "google") }
func GitHubCallback(w http.ResponseWriter, r *http.Request) { finishOAuth(w, r, "github") }

func oauthCookie(r *http.Request, provider, value string, age int) *http.Cookie {
	return &http.Cookie{Name: "oauth_" + provider, Value: value, Path: "/", HttpOnly: true, Secure: secureRequest(r), SameSite: http.SameSiteLaxMode, MaxAge: age}
}

func beginOAuth(w http.ResponseWriter, r *http.Request, provider string) {
	if r.Method != http.MethodGet {
		ErrorHandler(w, "Method not allowed", 405)
		return
	}
	beginOAuthFlow(w, r, provider, 0, "")
}
func ConnectOAuthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ErrorHandler(w, "Method not allowed", 405)
		return
	}
	provider := r.URL.Query().Get("provider")
	if provider != "google" && provider != "github" {
		ErrorHandler(w, "Unknown provider", 400)
		return
	}
	cookie, _ := GetSessionID(w, r)
	user, ok := lookupSessionRequest(r, cookie.Value)
	if !ok {
		ErrorHandler(w, "Sign in before connecting a provider", 401)
		return
	}
	beginOAuthFlow(w, r, provider, user.ID, cookie.Value)
}
func beginOAuthFlow(w http.ResponseWriter, r *http.Request, provider string, userID int, session string) {
	cfg, err := oauthConfig(provider)
	if err != nil {
		renderAuth(w, "login.html", 503, authForm{Check: err.Error()})
		return
	}
	db, release, err := requestDB(r)
	if err != nil {
		ErrorHandler(w, "Database error", 500)
		return
	}
	defer release()
	state, verifier := randomToken(), oauth2.GenerateVerifier()
	var linkID interface{}
	sessionHash := ""
	if userID > 0 {
		linkID = userID
		sessionHash = tokenHash(session)
	}
	if _, err = db.Exec("INSERT INTO oauth_flows(state_hash,provider,verifier,expires_at,link_user_id,session_hash) VALUES(?,?,?,?,?,?)", tokenHash(state), provider, verifier, time.Now().UTC().Add(10*time.Minute).Format("2006-01-02 15:04:05"), linkID, sessionHash); err != nil {
		ErrorHandler(w, "Unable to start sign-in", 500)
		return
	}
	http.SetCookie(w, oauthCookie(r, provider, state, 600))
	w.Header().Set("Cache-Control", "no-store")
	http.Redirect(w, r, cfg.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier)), 302)
}

func finishOAuth(w http.ResponseWriter, r *http.Request, provider string) {
	if r.Method != http.MethodGet {
		ErrorHandler(w, "Method not allowed", 405)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	cookie, err := r.Cookie("oauth_" + provider)
	http.SetCookie(w, oauthCookie(r, provider, "", -1))

	state := r.URL.Query().Get("state")
	if err != nil || len(state) < 32 || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 {
		renderAuth(w, "login.html", 400, authForm{Check: "Sign-in expired or could not be verified. Please try again."})
		return
	}
	db, release, err := requestDB(r)
	if err != nil {
		ErrorHandler(w, "Database error", 500)
		return
	}
	defer release()
	var verifier, sessionHash string
	var linkID sql.NullInt64
	if err = db.QueryRow("DELETE FROM oauth_flows WHERE state_hash=? AND provider=? AND expires_at>CURRENT_TIMESTAMP RETURNING verifier,link_user_id,session_hash", tokenHash(state), provider).Scan(&verifier, &linkID, &sessionHash); err != nil {
		renderAuth(w, "login.html", 400, authForm{Check: "Sign-in expired or has already been used. Please try again."})
		return
	}
	if linkID.Valid {
		session, err := r.Cookie("session_id")
		if err != nil || subtle.ConstantTimeCompare([]byte(tokenHash(session.Value)), []byte(sessionHash)) != 1 {
			ErrorHandler(w, "Your session changed. Start account linking again.", 403)
			return
		}
		user, ok := lookupSessionRequest(r, session.Value)
		if !ok || user.ID != int(linkID.Int64) {
			ErrorHandler(w, "Sign in again before connecting this provider", 403)
			return
		}
	}
	if r.URL.Query().Get("error") != "" {
		renderAuth(w, "login.html", 400, authForm{Check: "Sign-in was cancelled or permission was not granted. You can try again."})
		return
	}
	code := r.URL.Query().Get("code")
	if code == "" {
		renderAuth(w, "login.html", 400, authForm{Check: "No authorization code was received. Please try again."})
		return
	}
	cfg, err := oauthConfig(provider)
	if err != nil {
		renderAuth(w, "login.html", 503, authForm{Check: err.Error()})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	ctx = context.WithValue(ctx, oauth2.HTTPClient, oauthHTTPClient)
	token, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		renderAuth(w, "login.html", 502, authForm{Check: "Unable to complete provider sign-in. Please retry or use email sign-in."})
		return
	}
	client := cfg.Client(ctx, token)
	identity, err := fetchIdentity(ctx, client, provider)
	if err != nil {
		renderAuth(w, "login.html", 502, authForm{Check: err.Error()})
		return
	}

	if linkID.Valid {
		if err = linkIdentity(db, int(linkID.Int64), identity); err != nil {
			renderAuth(w, "login.html", 409, authForm{Check: "This provider identity is already connected to another account, or could not be linked."})
			return
		}
		http.Redirect(w, r, "/profile?notice=linked", 303)
		return
	}
	userID, err := resolveIdentityRequest(r, identity)
	if err != nil {
		status := 500
		message := "Unable to save your account. Please try again."
		if errors.Is(err, errEmailRegistered) {
			status = 409
			message = "An account with this email already exists. Sign in using its original method. Accounts are not linked automatically."
		}
		renderAuth(w, "login.html", status, authForm{Check: message})
		return
	}
	if err = startSession(w, r, userID); err != nil {
		ErrorHandler(w, "Unable to create session", 500)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

type providerIdentity struct{ Provider, Subject, Email, Name string }

func getProviderJSON(ctx context.Context, client *http.Client, endpoint string, target interface{}) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Forum")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("Identity provider returned HTTP %d", resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(target)
}

func fetchIdentity(ctx context.Context, client *http.Client, provider string) (providerIdentity, error) {
	result := providerIdentity{Provider: provider}
	if provider == "google" {
		var info struct {
			Sub, Email, Name string
			Verified         bool `json:"email_verified"`
		}
		if err := getProviderJSON(ctx, client, "https://www.googleapis.com/oauth2/v3/userinfo", &info); err != nil {
			return result, errors.New("Unable to fetch your Google profile. Please try again.")
		}
		if info.Sub == "" || info.Email == "" || !info.Verified {
			return result, errors.New("A verified Google email address is required.")
		}
		result.Subject = info.Sub
		result.Email = info.Email
		result.Name = info.Name
	} else {
		var info struct {
			ID    int64
			Login string
		}
		if err := getProviderJSON(ctx, client, "https://api.github.com/user", &info); err != nil {
			return result, errors.New("Unable to fetch your GitHub profile. Please try again.")
		}
		if info.ID == 0 {
			return result, errors.New("GitHub did not return an account identifier.")
		}
		var emails []struct {
			Email             string
			Primary, Verified bool
		}
		if err := getProviderJSON(ctx, client, "https://api.github.com/user/emails", &emails); err != nil {
			return result, errors.New("Unable to read your GitHub email. Please grant email access and retry.")
		}
		for _, email := range emails {
			if email.Verified && (result.Email == "" || email.Primary) {
				result.Email = email.Email
				if email.Primary {
					break
				}
			}
		}
		if result.Email == "" {
			return result, errors.New("Add and verify an email address on GitHub, then try again.")
		}
		result.Subject = strconv.FormatInt(info.ID, 10)
		result.Name = info.Login
	}
	result.Email = strings.ToLower(strings.TrimSpace(result.Email))
	return result, nil
}

var errEmailRegistered = errors.New("email already registered")
var invalidUsername = regexp.MustCompile(`[^a-zA-Z0-9_]`)

func resolveIdentity(identity providerIdentity) (int, error) {
	return resolveIdentityRequest(nil, identity)
}
func resolveIdentityRequest(r *http.Request, identity providerIdentity) (int, error) {
	db, release, err := requestDB(r)
	if err != nil {
		return 0, err
	}
	defer release()
	var userID int
	err = db.QueryRow("SELECT user_id FROM oauth_accounts WHERE provider=? AND subject=?", identity.Provider, identity.Subject).Scan(&userID)
	if err == nil {
		return userID, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var count int
	if err = tx.QueryRow("SELECT COUNT(*) FROM users WHERE LOWER(email)=LOWER(?)", identity.Email).Scan(&count); err != nil {
		return 0, err
	}
	if count != 0 {
		return 0, errEmailRegistered
	}
	base := invalidUsername.ReplaceAllString(identity.Name, "_")
	if len(base) < 3 {
		base = identity.Provider + "_user"
	}
	if len(base) > 12 {
		base = base[:12]
	}
	username := base
	for attempt := 0; attempt < 5; attempt++ {
		if err = tx.QueryRow("SELECT COUNT(*) FROM users WHERE username=?", username).Scan(&count); err != nil {
			return 0, err
		}
		if count == 0 {
			break
		}
		username = base + "_" + randomToken()[:6]
	}
	// Invalid bcrypt marker: social accounts have no usable local password.
	result, err := tx.Exec("INSERT INTO users(email,username,password,role,email_verified) VALUES(?,?,?,'user',1)", identity.Email, username, "!oauth")
	if err != nil {
		return 0, err
	}
	newID, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if _, err = tx.Exec("INSERT INTO oauth_accounts(provider,subject,user_id) VALUES(?,?,?)", identity.Provider, identity.Subject, newID); err != nil {
		return 0, err
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return int(newID), nil
}

func linkIdentity(db *sql.DB, userID int, identity providerIdentity) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner int
	err = tx.QueryRow("SELECT user_id FROM oauth_accounts WHERE provider=? AND subject=?", identity.Provider, identity.Subject).Scan(&owner)
	if err == nil && owner != userID {
		return fmt.Errorf("identity already linked")
	}
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if _, err = tx.Exec("INSERT INTO oauth_accounts(provider,subject,user_id) VALUES(?,?,?) ON CONFLICT(provider,subject) DO NOTHING", identity.Provider, identity.Subject, userID); err != nil {
		return err
	}
	return tx.Commit()
}

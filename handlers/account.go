package handlers

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"golang.org/x/crypto/bcrypt"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

func tokenHash(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}
func baseURL() string {
	base := strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/")
	if base == "" {
		base = "https://localhost:4000"
	}
	return base
}
func renderAccount(w http.ResponseWriter, status int, mode, token, message, notice string) {
	data := map[string]interface{}{"Mode": mode, "Token": token, "Check": message, "Notice": notice}
	tmpl, err := parsePage("account_action.html")
	if err != nil {
		ErrorHandler(w, "Template error", 500)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	// Keep reset/verification URLs private from other sites while allowing
	// the same-origin Referer fallback used by the request-origin check.
	w.Header().Set("Referrer-Policy", "same-origin")
	// Render before committing status so a template failure cannot produce a partial page.
	body, err := renderTemplateBytes(tmpl, data)
	if err != nil {
		ErrorHandler(w, "Unable to render account page", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

var sendAccountMail = deliverMail

func issueAccountToken(db *sql.DB, userID int, purpose, email string) error {
	var count int
	// Avoid repeated email requests for the same purpose within thirty seconds.
	duration := 15 * time.Minute
	if purpose == "verify" {
		duration = 24 * time.Hour
	}
	threshold := time.Now().UTC().Add(duration - 30*time.Second).Format("2006-01-02 15:04:05")
	if err := db.QueryRow("SELECT COUNT(*) FROM account_tokens WHERE user_id=? AND purpose=? AND expires_at>?", userID, purpose, threshold).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	token := randomToken()
	expires := time.Now().UTC().Add(duration).Format("2006-01-02 15:04:05")
	if _, err := db.Exec("INSERT INTO account_tokens(token_hash,user_id,purpose,expires_at) VALUES(?,?,?,?)", tokenHash(token), userID, purpose, expires); err != nil {
		return err
	}
	path, subject := "/reset-password", "Reset your Forum password"
	if purpose == "verify" {
		path, subject = "/verify-email", "Verify your Forum email"
	}
	body := "Use the following link to continue:\n\n" + baseURL() + path + "?token=" + token + "\n\nIf you did not request this, ignore this email.\n"
	if err := sendAccountMail(email, subject, body); err != nil {
		_, _ = db.Exec("DELETE FROM account_tokens WHERE token_hash=?", tokenHash(token))
		return err
	}
	return nil
}
func ForgotPasswordHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		renderAccount(w, 200, "forgot", "", "", "")
		return
	}
	if r.Method != http.MethodPost {
		ErrorHandler(w, "Method not allowed", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := r.ParseForm(); err != nil {
		ErrorHandler(w, "Invalid form", 400)
		return
	}
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	db, release, err := requestDB(r)
	if err != nil {
		ErrorHandler(w, "Database error", 500)
		return
	}
	defer release()
	user, err := databaseUserByEmail(db, email)
	if err != nil {
		log.Printf("password-reset lookup: %v", err)
	} else if user.ID != 0 {
		if err = issueAccountToken(db, user.ID, "reset", email); err != nil {
			log.Printf("password-reset delivery failed: %v", err)
		}
	}
	renderAccount(w, 200, "forgot", "", "", "If that account exists, a password reset link has been sent. Check your inbox.")
}

type accountUser struct {
	ID    int
	Email string
}

func databaseUserByEmail(db *sql.DB, email string) (accountUser, error) {
	var u accountUser
	err := db.QueryRow("SELECT id,email FROM users WHERE LOWER(email)=LOWER(?)", email).Scan(&u.ID, &u.Email)
	if err == sql.ErrNoRows {
		return accountUser{}, nil
	}
	return u, err
}
func ResetPasswordHandler(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if r.Method == http.MethodGet {
		renderAccount(w, 200, "reset", token, "", "")
		return
	}
	if r.Method != http.MethodPost {
		ErrorHandler(w, "Method not allowed", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := r.ParseForm(); err != nil {
		ErrorHandler(w, "Invalid form", 400)
		return
	}
	token = r.FormValue("token")
	password := r.FormValue("password")
	if err := validateCredentials(RegisterRequest{Email: "valid@example.test", Username: "valid_user", Password: password}); err != nil {
		renderAccount(w, 422, "reset", token, err.Error(), "")
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		ErrorHandler(w, "Unable to use password", 422)
		return
	}
	db, release, err := requestDB(r)
	if err != nil {
		ErrorHandler(w, "Database error", 500)
		return
	}
	defer release()
	tx, err := db.Begin()
	if err != nil {
		ErrorHandler(w, "Database error", 500)
		return
	}
	defer tx.Rollback()
	var userID int
	if err = tx.QueryRow("DELETE FROM account_tokens WHERE token_hash=? AND purpose='reset' AND expires_at>CURRENT_TIMESTAMP RETURNING user_id", tokenHash(token)).Scan(&userID); err != nil {
		renderAccount(w, 400, "reset", "", "This reset link is invalid or expired. Request a new one.", "")
		return
	}
	var accountEmail string
	if err = tx.QueryRow("SELECT email FROM users WHERE id=?", userID).Scan(&accountEmail); err != nil {
		ErrorHandler(w, "Unable to read account", 500)
		return
	}
	for _, query := range []struct {
		sql  string
		args []interface{}
	}{
		{"UPDATE users SET password=?,email_verified=1 WHERE id=?", []interface{}{string(hash), userID}},
		{"DELETE FROM sessions WHERE user_id=?", []interface{}{userID}},
		{"DELETE FROM account_tokens WHERE user_id=?", []interface{}{userID}},
		{"DELETE FROM login_attempts WHERE account_hash=?", []interface{}{tokenHash(strings.ToLower(accountEmail))}},
	} {
		if _, err = tx.Exec(query.sql, query.args...); err != nil {
			ErrorHandler(w, "Unable to update password", 500)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		ErrorHandler(w, "Unable to update password", 500)
		return
	}
	renderAccount(w, 200, "done", "", "", "Your password has been changed. Sign in again with your new password.")
}
func RequestVerificationHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ErrorHandler(w, "Method not allowed", 405)
		return
	}
	cookie, _ := GetSessionID(w, r)
	user, ok := lookupSessionRequest(r, cookie.Value)
	if !ok {
		ErrorHandler(w, "Sign in first", 401)
		return
	}
	db, release, err := requestDB(r)
	if err != nil {
		ErrorHandler(w, "Database error", 500)
		return
	}
	defer release()
	var email string
	var verified bool
	if err = db.QueryRow("SELECT email,email_verified FROM users WHERE id=?", user.ID).Scan(&email, &verified); err != nil {
		ErrorHandler(w, "Account not found", 404)
		return
	}
	if !verified {
		if err = issueAccountToken(db, user.ID, "verify", email); err != nil {
			log.Printf("verification delivery: %v", err)
			ErrorHandler(w, "Email delivery is not configured or is temporarily unavailable", 503)
			return
		}
	}
	http.Redirect(w, r, "/profile?notice=verification", 303)
}
func VerifyEmailHandler(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if r.Method == http.MethodGet {
		renderAccount(w, 200, "verify", token, "", "")
		return
	}
	if r.Method != http.MethodPost {
		ErrorHandler(w, "Method not allowed", 405)
		return
	}
	token = r.FormValue("token")
	db, release, err := requestDB(r)
	if err != nil {
		ErrorHandler(w, "Database error", 500)
		return
	}
	defer release()
	tx, err := db.Begin()
	if err != nil {
		ErrorHandler(w, "Database error", 500)
		return
	}
	defer tx.Rollback()
	var userID int
	if err = tx.QueryRow("DELETE FROM account_tokens WHERE token_hash=? AND purpose='verify' AND expires_at>CURRENT_TIMESTAMP RETURNING user_id", tokenHash(token)).Scan(&userID); err != nil {
		renderAccount(w, 400, "verify", "", "This verification link is invalid or expired.", "")
		return
	}
	if _, err = tx.Exec("UPDATE users SET email_verified=1 WHERE id=?", userID); err != nil {
		ErrorHandler(w, "Database error", 500)
		return
	}
	if err = tx.Commit(); err != nil {
		ErrorHandler(w, "Database error", 500)
		return
	}
	renderAccount(w, 200, "done", "", "", "Email verified. You can now sign in.")
}
func loginBlocked(db *sql.DB, email string) (bool, error) {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM login_attempts WHERE account_hash=? AND blocked_until>CURRENT_TIMESTAMP", tokenHash(email)).Scan(&count)
	return count > 0, err
}
func recordLoginFailure(db *sql.DB, email string) error {
	_, err := db.Exec(`INSERT INTO login_attempts(account_hash,failures,updated_at) VALUES(?,1,CURRENT_TIMESTAMP) ON CONFLICT(account_hash) DO UPDATE SET failures=CASE WHEN updated_at<datetime('now','-15 minutes') THEN 1 ELSE failures+1 END,blocked_until=CASE WHEN failures>=4 AND updated_at>=datetime('now','-15 minutes') THEN datetime('now','+15 minutes') ELSE NULL END,updated_at=CURRENT_TIMESTAMP`, tokenHash(email))
	return err
}

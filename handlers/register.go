package handlers

import (
	"golang.org/x/crypto/bcrypt"
	"log"
	"net/http"
	"os"
	"strings"
)

type RegisterRequest struct{ Email, Username, Password string }

func RegisterHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		renderAuth(w, "register.html", 200, authForm{})
		return
	}
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, POST")
		ErrorHandler(w, "Method not allowed", 405)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := r.ParseForm(); err != nil {
		ErrorHandler(w, "Invalid form", 400)
		return
	}
	req := RegisterRequest{strings.ToLower(strings.TrimSpace(r.FormValue("email"))), strings.TrimSpace(r.FormValue("username")), r.FormValue("password")}
	data := authForm{Email: req.Email, Username: req.Username}
	invalid := func(message string) { data.Check = message; renderAuth(w, "register.html", 422, data) }
	if err := validateCredentials(req); err != nil {
		invalid(err.Error())
		return
	}
	db, release, err := requestDB(r)
	if err != nil {
		ErrorHandler(w, "Database connection error", 500)
		return
	}
	defer release()
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM users WHERE LOWER(email)=LOWER(?)", req.Email).Scan(&count); err != nil {
		ErrorHandler(w, "Database error", 500)
		return
	}
	if count != 0 {
		invalid("Email is already in use. Sign in to your existing account.")
		return
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM users WHERE username=?", req.Username).Scan(&count); err != nil {
		ErrorHandler(w, "Database error", 500)
		return
	}
	if count != 0 {
		invalid("This username is already taken.")
		return
	}
	hashed, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		invalid("Unable to use this password. Choose a password of at most 72 bytes.")
		return
	}
	if _, err = db.Exec("INSERT INTO users (email,username,password,role,email_verified) VALUES (?,?,?,'user',0)", req.Email, req.Username, string(hashed)); err != nil {
		invalid("Unable to create this account. The email or username may already be in use.")
		return
	}
	if os.Getenv("SMTP_HOST") != "" || os.Getenv("FORUM_DEV_MAIL") == "1" {
		var userID int
		if err = db.QueryRow("SELECT id FROM users WHERE email=?", req.Email).Scan(&userID); err == nil {
			if err = issueAccountToken(db, userID, "verify", req.Email); err != nil {
				log.Printf("verification delivery failed: %v", err)
			}
		}
	}
	http.Redirect(w, r, "/login?registered=1", http.StatusSeeOther)
}

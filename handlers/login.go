package handlers

import (
	"forum/database"
	"net/http"
	"os"
	"strings"
)

func LoginHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		renderAuth(w, "login.html", 200, authForm{})
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
	email := strings.ToLower(strings.TrimSpace(r.FormValue("email")))
	password := r.FormValue("password")
	data := authForm{Email: email}
	if email == "" || password == "" {
		data.Check = "Email and password are required."
		renderAuth(w, "login.html", 422, data)
		return
	}
	db, release, err := requestDB(r)
	if err != nil {
		ErrorHandler(w, "Database connection error", 500)
		return
	}
	defer release()
	blocked, err := loginBlocked(db, email)
	if err != nil {
		ErrorHandler(w, "Unable to check sign-in limits", 500)
		return
	}
	if blocked {
		w.Header().Set("Retry-After", "900")
		renderAuth(w, "login.html", 429, authForm{Email: email, Check: "Too many failed sign-in attempts. Try again in 15 minutes or reset your password."})
		return
	}
	user, err := database.GetUserByEmail(db, email)
	if err != nil {
		ErrorHandler(w, "Database error", 500)
		return
	}
	if user == nil || database.ComparePassword(user.PasswordHash, password) != nil {
		if err := recordLoginFailure(db, email); err != nil {
			ErrorHandler(w, "Unable to record sign-in attempt", 500)
			return
		}
		data.Check = "Invalid email or password. For a social account, use its provider button."
		renderAuth(w, "login.html", 401, data)
		return
	}
	if os.Getenv("REQUIRE_EMAIL_VERIFICATION") == "1" {
		var verified bool
		if err = db.QueryRow("SELECT email_verified FROM users WHERE id=?", user.ID).Scan(&verified); err != nil {
			ErrorHandler(w, "Database error", 500)
			return
		}
		if !verified {
			renderAuth(w, "login.html", 403, authForm{Email: email, Check: "Verify your email before signing in. Use the link sent when you registered, or request a password reset to verify and recover your account."})
			return
		}
	}
	if _, err = db.Exec("DELETE FROM login_attempts WHERE account_hash=?", tokenHash(email)); err != nil {
		ErrorHandler(w, "Database error", 500)
		return
	}
	if err = startSession(w, r, user.ID); err != nil {
		ErrorHandler(w, "Unable to create session", 500)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

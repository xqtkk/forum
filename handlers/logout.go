package handlers

import (
	"net/http"
)

func LogoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		ErrorHandler(w, "Method not allowed", 405)
		return
	}
	if cookie, err := r.Cookie("session_id"); err == nil {
		db, release, err := requestDB(r)
		if err != nil {
			ErrorHandler(w, "Database error", 500)
			return
		}
		defer release()
		if _, err = db.Exec("DELETE FROM sessions WHERE session_id=?", cookie.Value); err != nil {
			ErrorHandler(w, "Unable to sign out", 500)
			return
		}
	}
	http.SetCookie(w, sessionCookie(r, "", -1))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

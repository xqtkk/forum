package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"forum/serverconfig"
	"net/http"
	"strings"
	"time"
)

func sessionLifetime() time.Duration {
	lifetime, err := serverconfig.SessionLifetime()
	if err != nil {
		return 24 * time.Hour
	}
	return lifetime
}

func sessionLifetimeSeconds() int {
	return int(sessionLifetime().Seconds())
}

func randomSessionID() string {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		panic(err)
	}
	id[6] = id[6]&0x0f | 0x40
	id[8] = id[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", id[0:4], id[4:6], id[6:8], id[8:10], id[10:16])
}

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func sessionCookie(r *http.Request, value string, age int) *http.Cookie {
	return &http.Cookie{Name: "session_id", Value: value, Path: "/", HttpOnly: true, Secure: secureRequest(r), SameSite: http.SameSiteLaxMode, MaxAge: age, Expires: time.Now().Add(time.Duration(age) * time.Second)}
}
func GetSessionID(w http.ResponseWriter, r *http.Request) (*http.Cookie, error) {
	cookie, err := r.Cookie("session_id")
	if err != nil || cookie.Value == "" {
		cookie = sessionCookie(r, randomToken(), sessionLifetimeSeconds())
		http.SetCookie(w, cookie)
	}
	return cookie, nil
}

type sessionUser struct {
	ID      int
	Initial string
}

func lookupSession(token string) (sessionUser, bool) { return lookupSessionRequest(nil, token) }
func lookupSessionRequest(r *http.Request, token string) (sessionUser, bool) {
	db, release, err := requestDB(r)
	if err != nil {
		return sessionUser{}, false
	}
	defer release()
	var user sessionUser
	var username string
	err = db.QueryRow(`SELECT u.id,u.username FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.session_id=? AND s.expires_at>CURRENT_TIMESTAMP`, token).Scan(&user.ID, &username)
	if err != nil {
		return sessionUser{}, false
	}
	for _, initial := range username {
		user.Initial = strings.ToUpper(string(initial))
		break
	}
	return user, true
}
func startSession(w http.ResponseWriter, r *http.Request, userID int) error {
	db, release, err := requestDB(r)
	if err != nil {
		return err
	}
	defer release()
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("DELETE FROM sessions WHERE expires_at<=CURRENT_TIMESTAMP"); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM sessions WHERE user_id=?", userID); err != nil {
		return err
	}
	token := randomSessionID()
	expires := time.Now().UTC().Add(sessionLifetime()).Format("2006-01-02 15:04:05")
	if _, err = tx.Exec("INSERT INTO sessions(session_id,user_id,expires_at) VALUES(?,?,?)", token, userID, expires); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	http.SetCookie(w, sessionCookie(r, token, sessionLifetimeSeconds()))
	return nil
}

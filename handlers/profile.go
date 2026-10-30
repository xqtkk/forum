package handlers

import (
	"forum/database"
	"net/http"
	"strconv"
	"strings"
)

func pageNumber(r *http.Request) int {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 1000000 {
		page = 1000000
	}
	return page
}
func setPagination(data map[string]interface{}, r *http.Request, page, total int) {
	pages := (total + 19) / 20
	if pages < 1 {
		pages = 1
	}
	data["Page"] = page
	data["TotalPages"] = pages
	address := func(n int) string {
		q := r.URL.Query()
		q.Set("page", strconv.Itoa(n))
		return r.URL.Path + "?" + q.Encode()
	}
	if page > 1 {
		data["PreviousURL"] = address(page - 1)
	}
	if page < pages {
		data["NextURL"] = address(page + 1)
	}
}
func ProfileHandler(w http.ResponseWriter, r *http.Request) {
	cookie, _ := GetSessionID(w, r)
	viewer, loggedIn := lookupSessionRequest(r, cookie.Value)
	target := viewer.ID
	if raw := r.URL.Query().Get("id"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 {
			ErrorHandler(w, "Invalid profile", 400)
			return
		}
		target = parsed
	}
	if target == 0 {
		http.Redirect(w, r, "/login", 303)
		return
	}
	db, release, err := requestDB(r)
	if err != nil {
		ErrorHandler(w, "Database error", 500)
		return
	}
	defer release()
	own := loggedIn && viewer.ID == target
	if r.Method == http.MethodPost {
		if !own {
			ErrorHandler(w, "You can edit only your own profile", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if err = r.ParseForm(); err != nil {
			ErrorHandler(w, "Invalid form", 400)
			return
		}
		if err = database.UpdateBio(db, viewer.ID, strings.TrimSpace(r.FormValue("bio"))); err != nil {
			ErrorHandler(w, err.Error(), 422)
			return
		}
		http.Redirect(w, r, "/profile?id="+strconv.Itoa(target), 303)
		return
	}
	if r.Method != http.MethodGet {
		ErrorHandler(w, "Method not allowed", 405)
		return
	}
	profile, err := database.GetProfile(db, target)
	if err != nil {
		ErrorHandler(w, "Profile not found", 404)
		return
	}
	role := ""
	if loggedIn {
		role, err = GetUserRole(db, viewer.ID)
		if err != nil {
			ErrorHandler(w, "Unable to read user", 500)
			return
		}
	}
	tab := r.URL.Query().Get("tab")
	if tab != "comments" {
		tab = "posts"
	}
	page := pageNumber(r)
	posts, err := database.ProfilePosts(db, target, page)
	if err != nil {
		ErrorHandler(w, "Unable to load posts", 500)
		return
	}
	comments, err := database.ProfileComments(db, target, page)
	if err != nil {
		ErrorHandler(w, "Unable to load comments", 500)
		return
	}
	data := map[string]interface{}{"Username": viewer.Initial, "Profile": profile, "Own": own, "LoggedIn": loggedIn, "Moder": role == "moderator", "Admin": role == "admin", "Posts": posts, "Comments": comments, "Tab": tab, "GoogleEnabled": oauthConfigured("google"), "GitHubEnabled": oauthConfigured("github")}
	total := profile.PostCount
	if tab == "comments" {
		total = profile.CommentCount
	}
	setPagination(data, r, page, total)
	if own {
		var email string
		var verified bool
		if err = db.QueryRow("SELECT email,email_verified FROM users WHERE id=?", viewer.ID).Scan(&email, &verified); err != nil {
			ErrorHandler(w, "Unable to read account", 500)
			return
		}
		data["AccountEmail"], data["EmailVerified"] = email, verified
		rows, e := db.Query("SELECT provider FROM oauth_accounts WHERE user_id=?", viewer.ID)
		if e != nil {
			ErrorHandler(w, "Unable to read connections", 500)
			return
		}
		for rows.Next() {
			var provider string
			if err = rows.Scan(&provider); err != nil {
				rows.Close()
				ErrorHandler(w, "Unable to read connections", 500)
				return
			}
			data["Connected"+strings.ToUpper(provider[:1])+provider[1:]] = true
		}
		rows.Close()
		data["Notice"] = map[string]string{"linked": "Provider connected successfully.", "verification": "Verification email requested. Check your inbox.", "password": "Password updated."}[r.URL.Query().Get("notice")]
	}
	tmpl, err := parsePage("profile.html")
	if err != nil {
		ErrorHandler(w, "Template error", 500)
		return
	}
	if err = executeTemplate(w, tmpl, data); err != nil {
		ErrorHandler(w, "Unable to render profile", 500)
	}
}

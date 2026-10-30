package handlers

import (
	"forum/database"
	"net/http"
	"strconv"
	"time"
)

func EditPostHandler(w http.ResponseWriter, r *http.Request) {
	sessionID, err := GetSessionID(w, r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}

	sessionUser, loggedIn := lookupSessionRequest(r, sessionID.Value)
	username := sessionUser.Initial
	if !loggedIn {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	loggedInUserID := sessionUser.ID
	postIDStr := r.URL.Query().Get("id")
	if postIDStr == "" {
		ErrorHandler(w, "Post ID is required", http.StatusBadRequest)
		return
	}

	postID, err := strconv.Atoi(postIDStr)
	if err != nil {
		ErrorHandler(w, "Invalid post ID", http.StatusBadRequest)
		return
	}
	UserID := sessionUser.ID
	// A hidden button or user_id form field is not authorization.
	authDB, releaseAuth, authErr := requestDB(r)
	if authErr != nil {
		ErrorHandler(w, "Database error", 500)
		return
	}
	defer releaseAuth()
	var ownerID int
	if err := authDB.QueryRow("SELECT author_id FROM posts WHERE id=?", postID).Scan(&ownerID); err != nil {
		ErrorHandler(w, "Post not found", 404)
		return
	}
	role, err := GetUserRole(authDB, UserID)
	if err != nil {
		ErrorHandler(w, "Unable to check permissions", 500)
		return
	}
	if ownerID != UserID && role != "admin" && role != "moderator" {
		ErrorHandler(w, "You cannot edit this post", 403)
		return
	}

	// Если метод GET, отображаем форму для создания поста
	if r.Method == http.MethodGet {
		// Получаем список категорий из базы данных
		db, release, err := requestDB(r)
		if err != nil {
			ErrorHandler(w, "Database connection error", http.StatusInternalServerError)
			return
		}
		defer release()
		var role string
		role, err = GetUserRole(db, loggedInUserID)
		if err != nil {
			ErrorHandler(w, "Error fetching user role", http.StatusInternalServerError)
			return
		}
		status, err := database.GetPromotionStatus(db, UserID)
		if err != nil {
			ErrorHandler(w, "Error fetching promotion status", http.StatusInternalServerError)
			return
		}
		stat := true
		if status == "pending" {
			stat = false
		}
		Moders := false
		if role == "moderator" || role == "admin" {
			Moders = true
		}

		admin := false
		if role == "admin" {
			admin = true
		}

		// Получаем пост из базы данных
		post, err := database.GetPostByID(db, postID)
		if err != nil {
			ErrorHandler(w, "Post not found", http.StatusNotFound)
			return
		}

		categories, err := database.GetCategories(db)
		if err != nil {
			ErrorHandler(w, "Error fetching categories", http.StatusInternalServerError)
			return
		}

		// Передаем категории в шаблон
		tmpl, err := parsePage("edit_post.html")
		if err != nil {
			ErrorHandler(w, "Template parsing error", http.StatusInternalServerError)
			return
		}

		data := map[string]interface{}{
			"Username": username,
			"Moders":   Moders,
			"UserId":   loggedInUserID,
			"Admin":    admin,
			"Category": categories,
			"Check":    "",
			"Post":     post,
			"Stat":     stat,
		}

		err = executeTemplate(w, tmpl, data)
		if err != nil {
			ErrorHandler(w, "Error rendering template", http.StatusInternalServerError)
			return
		}
		return
	}

	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, maxPostBodySize)
		if err := r.ParseMultipartForm(2 << 20); err != nil {
			ErrorHandler(w, "Invalid form or image exceeds upload limit", 400)
			return
		}
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}

		title := r.FormValue("title")
		content := r.FormValue("content")

		categoryIDs, err := parseCategoryIDs(r.Form)
		if err != nil {
			ErrorHandler(w, err.Error(), http.StatusBadRequest)
			return
		}

		if title == "" || content == "" {
			ErrorHandler(w, "Title and content are required", 422)
			return
		}

		// Открываем соединение с базой данных
		db, release, err := requestDB(r)
		if err != nil {
			ErrorHandler(w, "Database connection error", http.StatusInternalServerError)
			return
		}
		defer release()

		if err := database.ValidatePost(title, content); err != nil {
			ErrorHandler(w, err.Error(), 422)
			return
		}
		savePath, err := savePostImage(r)
		if err != nil {
			ErrorHandler(w, err.Error(), 422)
			return
		}
		if savePath == "" {
			if err = db.QueryRow("SELECT COALESCE(image_path,'') FROM posts WHERE id=?", postID).Scan(&savePath); err != nil {
				ErrorHandler(w, "Unable to read image", 500)
				return
			}
		}

		// Вставляем пост в базу данных
		createdAt := time.Now().Format("2006-01-02 15:04:05")
		err = database.EditPost(db, title, content, categoryIDs, createdAt, postID, savePath)
		if err != nil {
			ErrorHandler(w, "Error saving post to database", http.StatusInternalServerError)
			return
		}

		// Перенаправляем на главную страницу или список постов
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	w.Header().Set("Allow", "GET, POST")
	ErrorHandler(w, "Method not allowed", http.StatusMethodNotAllowed)
}

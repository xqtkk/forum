package handlers

import (
	"forum/database"
	"net/http"
	"time"
)

// CreatePostHandler обрабатывает создание нового поста
func CreatePostHandler(w http.ResponseWriter, r *http.Request) {
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
	userID := sessionUser.ID
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
		Moders := false
		if role == "moderator" || role == "admin" {
			Moders = true
		}

		admin := false
		if role == "admin" {
			admin = true
		}
		categories, err := database.GetCategories(db)
		if err != nil {
			ErrorHandler(w, "Error fetching categories", http.StatusInternalServerError)
			return
		}
		status, err := database.GetPromotionStatus(db, userID)
		if err != nil {
			ErrorHandler(w, "Error fetching promotion status", http.StatusInternalServerError)
			return
		}
		stat := true
		if status == "pending" {
			stat = false
		}

		// Передаем категории в шаблон
		tmpl, err := parsePage("create_post.html")
		if err != nil {
			ErrorHandler(w, "Template parsing error", http.StatusInternalServerError)
			return
		}
		data := map[string]interface{}{
			"Username": username,
			"Moder":    Moders,
			"UserID":   loggedInUserID,
			"Admin":    admin,
			"Category": categories,
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
		userID := sessionUser.ID

		categoryIDs, err := parseCategoryIDs(r.Form)
		if err != nil {
			ErrorHandler(w, err.Error(), http.StatusBadRequest)
			return
		}

		if title == "" || content == "" {
			ErrorHandler(w, "Title and content are required", http.StatusUnprocessableEntity)
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

		// Вставляем пост в базу данных
		createdAt := time.Now().Format("2006-01-02 15:04:05")
		err = database.CreatePost(db, title, content, userID, categoryIDs, createdAt, savePath)
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

package handlers

import (
	"forum/database"
	"net/http"
)

func LikePostHandler(w http.ResponseWriter, r *http.Request) {
	// Получаем сессию
	sessionID, err := GetSessionID(w, r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}

	// Проверяем, авторизован ли пользователь
	sessionUser, loggedIn := lookupSessionRequest(r, sessionID.Value)
	username := sessionUser.Initial
	if !loggedIn {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	// Получаем ID пользователя по имени
	UserID, ok := sessionUser.ID, loggedIn
	if !ok {
		ErrorHandler(w, "User not found", http.StatusUnauthorized)
		return
	}

	// Открываем соединение с базой данных
	db, release, err := requestDB(r)
	if err != nil {
		ErrorHandler(w, "Database connection error", http.StatusInternalServerError)
		return
	}
	defer release()
	var role string
	role, err = GetUserRole(db, UserID)
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
	// Получаем посты, которые пользователь лайкнул
	posts, err := database.GetLikedPostsByUserID(db, UserID)
	if err != nil {
		ErrorHandler(w, "Error retrieving user's liked posts", http.StatusInternalServerError)
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

	// Загружаем шаблон для отображения постов
	tmpl, err := parsePage("my_posts.html")
	if err != nil {
		ErrorHandler(w, "Template parsing error", http.StatusInternalServerError)
		return
	}
	data := map[string]interface{}{
		"PageTitle": "Posts you liked",
		"LoggedIn":  loggedIn,
		"ID":        UserID,
		"Username":  username,
		"Posts":     posts,
		"Moder":     Moders,
		"Admin":     admin,
		"Stat":      stat,
	}
	// Рендерим шаблон
	err = executeTemplate(w, tmpl, data)
	if err != nil {
		ErrorHandler(w, "Error rendering template", http.StatusInternalServerError)
		return
	}
}

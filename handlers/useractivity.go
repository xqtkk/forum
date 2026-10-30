package handlers

import (
	"forum/database"
	"log"
	"net/http"
)

type Activity struct {
	ID             int
	AuthorID       int
	Type           string
	PostID         *int
	CommentContent *string
	CreatedAt      string
}

func GetUserActivity(w http.ResponseWriter, r *http.Request) {
	// Проверка сессии
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

	// Получаем ID пользователя по имени
	userID := sessionUser.ID

	query := `
	SELECT a.activity_type, CAST(a.created_at AS TEXT), p.id AS post_id, COALESCE(c.content,a.comment_content) AS comment_content
	FROM activities a
	LEFT JOIN posts p ON a.post_id = p.id
	LEFT JOIN comments c ON a.comment_id = c.id
	WHERE a.user_id = $1
	ORDER BY a.created_at DESC;
	`
	// Открываем соединение с базой данных
	db, release, err := requestDB(r)
	if err != nil {
		ErrorHandler(w, "Database connection error", http.StatusInternalServerError)
		return
	}
	defer release()
	var role string
	role, err = GetUserRole(db, userID)
	if err != nil {
		ErrorHandler(w, "Error fetching user role", http.StatusInternalServerError)
		return
	}
	rows, err := db.Query(query, userID)
	if err != nil {
		ErrorHandler(w, "Database query failed", http.StatusInternalServerError)
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
	defer rows.Close()

	var activities []Activity
	for rows.Next() {
		var activity Activity
		// Сканируем в указатели для корректной работы с NULL
		if err := rows.Scan(&activity.Type, &activity.CreatedAt, &activity.PostID, &activity.CommentContent); err != nil {
			log.Println(err)
			ErrorHandler(w, "Unable to read activity", 500)
			return
		}
		activities = append(activities, activity)
	}
	if err := rows.Err(); err != nil {
		ErrorHandler(w, "Unable to read activity", 500)
		return
	}
	rows.Close()
	status, err := database.GetPromotionStatus(db, userID)
	if err != nil {
		ErrorHandler(w, "Error fetching promotion status", http.StatusInternalServerError)
		return
	}
	stat := true
	if status == "pending" {
		stat = false
	}

	tmpl, err := parsePage("activity_page.html")
	if err != nil {
		ErrorHandler(w, "Failed to load template", http.StatusInternalServerError)
		return
	}

	// Передаем данные в шаблон
	data := map[string]interface{}{
		"Activities": activities,
		"Username":   username,
		"Moder":      Moders,
		"Admin":      admin,
		"Stat":       stat,
	}

	err = executeTemplate(w, tmpl, data)
	if err != nil {
		ErrorHandler(w, "Failed to render template", http.StatusInternalServerError)
		return
	}
}

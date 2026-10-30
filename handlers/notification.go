package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"forum/database"
	"log"
	"net/http"
	"strconv"
)

func NotificationCountHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		ErrorHandler(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	cookie, _ := GetSessionID(w, r)
	viewer, ok := lookupSessionRequest(r, cookie.Value)
	if !ok {
		http.Error(w, "Sign in required", http.StatusUnauthorized)
		return
	}
	db, release, err := requestDB(r)
	if err != nil {
		ErrorHandler(w, "Database connection error", http.StatusInternalServerError)
		return
	}
	defer release()
	var count int
	if err = db.QueryRow("SELECT COUNT(*) FROM notifications WHERE user_id=? AND is_read=0", viewer.ID).Scan(&count); err != nil {
		ErrorHandler(w, "Unable to count notifications", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]int{"count": count})
}

func NotificationsHandler(w http.ResponseWriter, r *http.Request) {
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
	SELECT n.id, n.notification_type, CAST(n.created_at AS TEXT), p.id AS post_id, COALESCE(c.content,n.comment_content) AS comment_content, n.is_read
	FROM notifications n
	LEFT JOIN posts p ON n.post_id = p.id
	LEFT JOIN comments c ON n.comment_id = c.id
	WHERE n.user_id = $1
	ORDER BY n.created_at DESC;
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
	Moders := false
	if role == "moderator" || role == "admin" {
		Moders = true
	}

	admin := false
	if role == "admin" {
		admin = true
	}

	if r.Method == http.MethodPost {
		// Получаем ID поста из параметров URL
		postIDStr := r.URL.Query().Get("id")
		if postIDStr == "" {
			ErrorHandler(w, "Post ID is required", http.StatusBadRequest)
			return
		}

		ID, err := strconv.Atoi(postIDStr)

		if err != nil {
			ErrorHandler(w, "Invalid post ID", http.StatusBadRequest)
			return
		}
		var owner int
		if err := db.QueryRow("SELECT user_id FROM notifications WHERE id=?", ID).Scan(&owner); err != nil || owner != userID {
			ErrorHandler(w, "Notification not found", 404)
			return
		}
		if err := handleNotificationActions(w, r, db, ID); err != nil {
			return
		}

		// Перенаправляем на страницу поста с добавленным комментарием
		http.Redirect(w, r, "/notifications", http.StatusSeeOther)
		return
	}
	rows, err := db.Query(query, userID)
	if err != nil {
		ErrorHandler(w, "Database query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var activities []Notification
	for rows.Next() {
		var activity Notification
		// Сканируем в указатели для корректной работы с NULL
		if err := rows.Scan(&activity.ID, &activity.Type, &activity.CreatedAt, &activity.PostID, &activity.CommentContent, &activity.IsRead); err != nil {
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

	tmpl, err := parsePage("notifications.html")
	if err != nil {
		log.Println("Error loading template:", err)
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

type Notification struct {
	ID             int
	Type           string
	PostID         *int
	CommentContent *string
	CreatedAt      string
	IsRead         bool
}

func handleNotificationActions(w http.ResponseWriter, r *http.Request, db *sql.DB, ID int) error {
	action := r.URL.Query().Get("action")
	switch action {
	case "read":
		if _, err := db.Exec("UPDATE notifications SET is_read=1 WHERE id=?", ID); err != nil {
			ErrorHandler(w, "Unable to update notification", 500)
			return err
		}
	default:
		ErrorHandler(w, "Invalid action", http.StatusBadRequest)
		return errors.New("invalid action")
	}

	return nil
}

func ReadNotificationsHandler(w http.ResponseWriter, r *http.Request) {
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
	SELECT n.id, n.notification_type, CAST(n.created_at AS TEXT), p.id AS post_id, COALESCE(c.content,n.comment_content) AS comment_content, n.is_read
	FROM notifications n
	LEFT JOIN posts p ON n.post_id = p.id
	LEFT JOIN comments c ON n.comment_id = c.id
	WHERE n.user_id = $1
	ORDER BY n.created_at DESC;
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
	Moders := false
	if role == "moderator" || role == "admin" {
		Moders = true
	}

	admin := false
	if role == "admin" {
		admin = true
	}

	rows, err := db.Query(query, userID)
	if err != nil {
		ErrorHandler(w, "Database query failed", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var activities []Notification
	for rows.Next() {
		var activity Notification
		// Сканируем в указатели для корректной работы с NULL
		if err := rows.Scan(&activity.ID, &activity.Type, &activity.CreatedAt, &activity.PostID, &activity.CommentContent, &activity.IsRead); err != nil {
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

	tmpl, err := parsePage("readnotifications.html")
	if err != nil {
		log.Println("Error loading template:", err)
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

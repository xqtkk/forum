package handlers

import (
	"database/sql"
	"forum/database" // Импортируй правильный путь к database
	"net/http"
	"strconv"
)

// Структура для хранения данных репорта
type Report struct {
	ID        int
	PostID    int
	PostTitle string
	Reporter  string
}

// ReportsHandler - обработчик для отображения открытых репортов
func ReportsHandler(w http.ResponseWriter, r *http.Request) {
	// Проверяем сессию пользователя
	sessionID, err := GetSessionID(w, r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}

	sessionUser, loggedIn := lookupSessionRequest(r, sessionID.Value)
	Username := sessionUser.Initial
	UserID := sessionUser.ID
	if !loggedIn {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	// Подключаемся к базе данных
	db, release, err := requestDB(r)
	if err != nil {
		ErrorHandler(w, "Database connection error", http.StatusInternalServerError)
		return
	}
	defer release()

	// Проверяем роль пользователя (только admin может видеть страницу)
	role, err := GetUserRole(db, UserID)
	if err != nil {
		ErrorHandler(w, "Error fetching user role", http.StatusInternalServerError)
		return
	}
	if role != "admin" {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	// Обработка POST-запросов (действия с репортами)
	if r.Method == http.MethodPost {
		action := r.URL.Query().Get("action")
		reportID := r.URL.Query().Get("id") // Получаем ID репорта

		if reportID == "" {
			ErrorHandler(w, "Missing report ID", http.StatusBadRequest)
			return
		}

		reportIDInt, err := strconv.Atoi(reportID)
		if err != nil {
			ErrorHandler(w, "Invalid report ID", http.StatusBadRequest)
			return
		}

		switch action {
		case "ignore":

			// Получаем post_id из репорта
			var postID int
			err := db.QueryRow("SELECT post_id FROM reports WHERE id = ?", reportIDInt).Scan(&postID)
			if err != nil {
				ErrorHandler(w, "Report not found", http.StatusNotFound)
				return
			}

			// Удаляем/обновляем репорт
			err = database.DeletePostReport(db, postID)
			if err != nil {
				ErrorHandler(w, "Error updating report status", http.StatusInternalServerError)
				return
			}

		default:
			ErrorHandler(w, "Invalid action", http.StatusBadRequest)
			return
		}

		// После обработки запроса перенаправляем обратно на страницу репортов
		http.Redirect(w, r, "/reports", http.StatusFound)
		return
	}

	// Получаем список репортов
	rows, err := db.Query(`
		SELECT reports.id, reports.post_id, COALESCE(posts.title, 'Deleted Post'), users.username
		FROM reports
		LEFT JOIN posts ON reports.post_id = posts.id
		LEFT JOIN users ON reports.reported_by = users.id
		WHERE reports.status = 'open'
	`)
	if err != nil {
		ErrorHandler(w, "Database query error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	// Создаём массив для хранения репортов
	var reports []Report
	for rows.Next() {
		var report Report
		var postTitle sql.NullString

		if err := rows.Scan(&report.ID, &report.PostID, &postTitle, &report.Reporter); err != nil {
			ErrorHandler(w, "Error scanning reports", http.StatusInternalServerError)
			return
		}

		if postTitle.Valid {
			report.PostTitle = postTitle.String
		} else {
			report.PostTitle = "Deleted Post"
		}

		reports = append(reports, report)
	}

	if err = rows.Err(); err != nil {
		ErrorHandler(w, "Database iteration error", http.StatusInternalServerError)
		return
	}

	// Загружаем HTML-шаблон
	tmpl, err := parsePage("reports.html")
	if err != nil {
		ErrorHandler(w, "Template parsing error", http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Username": Username,
		"Reports":  reports,
		"Admin":    true,
	}

	// Отправляем данные в шаблон
	err = executeTemplate(w, tmpl, data)
	if err != nil {
		ErrorHandler(w, "Template rendering error", http.StatusInternalServerError)
	}
}

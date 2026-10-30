package handlers

import (
	"net/http"
)

type User struct {
	ID       int
	Username string
	Email    string
	Role     string
}

func UserListHandler(w http.ResponseWriter, r *http.Request) {
	// Проверяем сессию пользователя
	sessionID, err := GetSessionID(w, r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
	sessionUser, loggedIn := lookupSessionRequest(r, sessionID.Value)
	username := sessionUser.Initial
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

	// Если это POST-запрос - выполняем изменение роли
	if r.Method == http.MethodPost {
		userID := r.URL.Query().Get("id")
		action := r.URL.Query().Get("action")

		switch action {
		case "promote":
			_, err := db.Exec(`UPDATE users SET role = 'moderator' WHERE id = ? AND role = 'user'`, userID)
			if err != nil {
				ErrorHandler(w, "Error promoting user", http.StatusInternalServerError)
				return
			}
		case "demote":
			_, err := db.Exec(`UPDATE users SET role = 'user' WHERE id = ? AND role = 'moderator'`, userID)
			if err != nil {
				ErrorHandler(w, "Error demoting user", http.StatusInternalServerError)
				return
			}
		default:
			ErrorHandler(w, "Invalid action", http.StatusBadRequest)
			return
		}

		http.Redirect(w, r, "/users", http.StatusSeeOther)
		return
	}

	// Получаем список пользователей
	rows, err := db.Query("SELECT id, username, email, role FROM users ORDER BY role DESC")
	if err != nil {
		ErrorHandler(w, "Error fetching users", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var users []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Role); err != nil {
			ErrorHandler(w, "Error scanning users", http.StatusInternalServerError)
			return
		}
		users = append(users, u)
	}

	// Загружаем шаблон
	tmpl, err := parsePage("users.html")
	if err != nil {
		ErrorHandler(w, "Template parsing error", http.StatusInternalServerError)
		return
	}
	data := map[string]interface{}{
		"Username": username,
		"Users":    users,
		"Admin":    true,
	}
	// Отправляем данные в шаблон
	err = executeTemplate(w, tmpl, data)
	if err != nil {
		ErrorHandler(w, "Template rendering error", http.StatusInternalServerError)
	}
}

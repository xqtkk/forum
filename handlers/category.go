package handlers

import (
	"forum/database"
	"net/http"
	"strings"
)

func CategoryHandler(w http.ResponseWriter, r *http.Request) {
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

	// Обработка POST-запросов (создание, редактирование, удаление)
	if r.Method == http.MethodPost {
		action := r.URL.Query().Get("action")
		if action == "create" || action == "edit" {
			name := strings.TrimSpace(r.FormValue("name"))
			if len([]rune(name)) < 1 || len([]rune(name)) > 60 {
				ErrorHandler(w, "Category names must contain 1–60 characters", 422)
				return
			}
		}
		switch action {
		case "create":
			categoryName := r.FormValue("name")
			if err := database.CreateCategory(db, strings.TrimSpace(categoryName)); err != nil {
				ErrorHandler(w, "Category already exists or could not be created", 409)
				return
			}
		case "edit":
			categoryID := r.URL.Query().Get("id")
			newName := r.FormValue("name")
			_, err := db.Exec("UPDATE categories SET name = ? WHERE id = ?", newName, categoryID)
			if err != nil {
				ErrorHandler(w, "Error updating category", http.StatusInternalServerError)
				return
			}
		case "delete":
			categoryID := r.URL.Query().Get("id")
			// Удаляем категорию
			_, err := db.Exec("DELETE FROM categories WHERE id = ?", categoryID)
			if err != nil {
				ErrorHandler(w, "Error deleting category", http.StatusInternalServerError)
				return
			}
		default:
			ErrorHandler(w, "Invalid action", http.StatusBadRequest)
			return
		}

		// После изменения перенаправляем обратно на страницу категорий
		http.Redirect(w, r, "/categories", http.StatusSeeOther)
		return
	}

	// Получаем список категорий
	rows, err := db.Query("SELECT id, name FROM categories")
	if err != nil {
		ErrorHandler(w, "Error fetching categories", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var categories []struct {
		ID   int
		Name string
	}

	for rows.Next() {
		var category struct {
			ID   int
			Name string
		}
		if err := rows.Scan(&category.ID, &category.Name); err != nil {
			ErrorHandler(w, "Error scanning categories", http.StatusInternalServerError)
			return
		}
		categories = append(categories, category)
	}

	// Загружаем HTML-шаблон
	tmpl, err := parsePage("categories.html")
	if err != nil {
		ErrorHandler(w, "Template parsing error", http.StatusInternalServerError)
		return
	}

	data := map[string]interface{}{
		"Username":   username,
		"Categories": categories,
		"Admin":      true,
	}

	// Отправляем данные в шаблон
	err = executeTemplate(w, tmpl, data)
	if err != nil {
		ErrorHandler(w, "Template rendering error", http.StatusInternalServerError)
	}
}

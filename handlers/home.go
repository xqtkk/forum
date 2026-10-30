package handlers

import (
	"forum/database"
	"net/http"
	"strconv"
)

// HomeHandler обрабатывает запросы на главную страницу
func HomeHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		ErrorHandler(w, "Page not found", 404)
		return
	}
	if r.Method != http.MethodGet {
		ErrorHandler(w, "Method not allowed", 405)
		return
	}
	// Получаем сессию пользователя
	sessionID, err := GetSessionID(w, r)
	if err != nil {
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}

	// Проверяем, авторизован ли пользователь
	sessionUser, loggedIn := lookupSessionRequest(r, sessionID.Value)
	username := sessionUser.Initial
	UserID := 0
	var role string
	if loggedIn {
		UserID = sessionUser.ID

		// Получаем роль пользователя
		db, release, err := requestDB(r)
		if err != nil {
			ErrorHandler(w, "Database connection error", http.StatusInternalServerError)
			return
		}
		defer release()

		role, err = GetUserRole(db, UserID)
		if err != nil {
			ErrorHandler(w, "Error fetching user role", http.StatusInternalServerError)
			return
		}
	}
	Moders := false
	if role == "moderator" || role == "admin" {
		Moders = true
	}

	admin := false
	if role == "admin" {
		admin = true
	}

	var categoryID *int
	categoryParam := r.URL.Query().Get("category_id")
	if r.URL.Query().Get("category_id") != "" {
		id, err := strconv.Atoi(categoryParam)
		if err != nil {
			ErrorHandler(w, "Invalid category ID", http.StatusBadRequest)
			return
		}
		categoryID = &id // Передаём указатель, если категория выбрана
	}

	// Получаем посты из базы данных
	db, release, err := requestDB(r)
	if err != nil {
		ErrorHandler(w, "Database connection error", http.StatusInternalServerError)
		return
	}
	defer release()

	page := pageNumber(r)
	posts, err := database.GetPostsPage(db, categoryID, page)
	if err != nil {
		ErrorHandler(w, "Error fetching posts", http.StatusInternalServerError)
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
	// Получаем категории из базы данных
	categories, err := database.GetCategories(db)
	if err != nil {
		ErrorHandler(w, "Error fetching categories", http.StatusInternalServerError)
		return
	}

	// Загружаем шаблон и передаем данные
	tmpl, err := parsePage("home.html")
	if err != nil {
		ErrorHandler(w, err.Error(), http.StatusInternalServerError)
		return
	}

	selectedCategory := 0
	if categoryID != nil {
		selectedCategory = *categoryID
	}
	data := map[string]interface{}{
		"SelectedCategory": selectedCategory,
		"LoggedIn":         loggedIn,
		"ID":               UserID,
		"Username":         username,
		"Posts":            posts,
		"Categories":       categories,
		"Moder":            Moders,
		"Admin":            admin,
		"Stat":             stat,
	}

	var total int
	countQuery := "SELECT COUNT(*) FROM posts"
	var args []interface{}
	if categoryID != nil {
		countQuery += " WHERE EXISTS(SELECT 1 FROM post_categories pc WHERE pc.post_id=posts.id AND pc.category_id=?) OR (NOT EXISTS(SELECT 1 FROM post_categories pc WHERE pc.post_id=posts.id) AND category_id=?)"
		args = append(args, *categoryID, *categoryID)
	}
	if err = db.QueryRow(countQuery, args...).Scan(&total); err != nil {
		ErrorHandler(w, "Unable to count posts", 500)
		return
	}
	data["TotalPosts"] = total
	setPagination(data, r, page, total)
	err = executeTemplate(w, tmpl, data)
	if err != nil {
		ErrorHandler(w, "Template rendering error", http.StatusInternalServerError)
	}
}

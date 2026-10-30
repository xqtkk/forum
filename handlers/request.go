package handlers

import (
	"forum/database"
	"net/http"
	"strconv"
	"strings"
)

func ShowPromotionFormHandler(w http.ResponseWriter, r *http.Request) {
	// Проверяем, что пользователь авторизован
	sessionID, err := GetSessionID(w, r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}

	// Получаем данные из сессии
	sessionUser, loggedIn := lookupSessionRequest(r, sessionID.Value)
	username := sessionUser.Initial
	if !loggedIn {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	userID := sessionUser.ID
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

	// Загружаем шаблон для формы подачи заявки
	tmpl, err := parsePage("formoder.html")
	if err != nil {
		ErrorHandler(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Передаем данные в шаблон (например, имя пользователя)
	data := map[string]interface{}{
		"Username": username,
		"Moder":    Moders,
		"Admin":    admin,
	}

	// Отображаем шаблон
	err = executeTemplate(w, tmpl, data)
	if err != nil {
		ErrorHandler(w, "Failed to render template", http.StatusInternalServerError)
		return
	}
}

func SubmitPromotionRequestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ErrorHandler(w, "Method not allowed", 405)
		return
	}
	// Проверяем, что пользователь авторизован
	sessionID, err := GetSessionID(w, r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}

	// Получаем данные из сессии
	sessionUser, loggedIn := lookupSessionRequest(r, sessionID.Value)
	username := sessionUser.Initial
	if !loggedIn {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	// Извлекаем информацию из формы
	reason := strings.TrimSpace(r.FormValue("reason"))
	if reason == "" || len([]rune(reason)) > 2000 {
		ErrorHandler(w, "Причина не может быть пустой", http.StatusBadRequest)
		return
	}
	userID := sessionUser.ID
	// Получаем ID пользователя из базы данных
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

	// Сохраняем заявку на повышение в базе данных
	_, err = db.Exec(`INSERT INTO promotion_requests (user_id, reason, status) VALUES (?, ?, 'pending')`, userID, reason)
	if err != nil {
		ErrorHandler(w, "Ошибка при подаче заявки", http.StatusInternalServerError)
		return
	}
	data := map[string]interface{}{
		"Username": username,
		"Moders":   Moders,
		"Admin":    admin,
	}
	// Загружаем шаблон с подтверждением подачи заявки
	tmpl, err := parsePage("formoders.html")
	if err != nil {
		ErrorHandler(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Отображаем шаблон подтверждения
	err = executeTemplate(w, tmpl, data)
	if err != nil {
		ErrorHandler(w, "Failed to render template", http.StatusInternalServerError)
		return
	}
}

func AdminPromotionRequestsHandler(w http.ResponseWriter, r *http.Request) {
	// Проверяем, что пользователь авторизован как админ
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

	// Извлекаем заявки из базы данных
	db, release, err := requestDB(r)
	if err != nil {
		ErrorHandler(w, "Database connection error", http.StatusInternalServerError)
		return
	}
	defer release()

	role, err := GetUserRole(db, UserID)
	if err != nil {
		ErrorHandler(w, "Error fetching user role", http.StatusInternalServerError)
		return
	}
	if role != "admin" {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	requests, err := database.GetPendingPromotionRequests(db)
	if err != nil {
		ErrorHandler(w, "Error fetching requests", http.StatusInternalServerError)
		return
	}
	data := map[string]interface{}{
		"Username": username,
		"Requests": requests,
		"Admin":    true,
	}
	// Загружаем шаблон и передаем данные
	tmpl, err := parsePage("admin_promotion_requests.html")
	if err != nil {
		ErrorHandler(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = executeTemplate(w, tmpl, data)
	if err != nil {
		ErrorHandler(w, "Failed to render template", http.StatusInternalServerError)
		return
	}
}

func ApproveRequestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ErrorHandler(w, "Method not allowed", 405)
		return
	}
	// Проверяем, что пользователь авторизован как админ
	sessionID, err := GetSessionID(w, r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}

	sessionUser, loggedIn := lookupSessionRequest(r, sessionID.Value)
	UserID := sessionUser.ID
	if !loggedIn {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	// Извлекаем ID заявки
	requestID, err := strconv.Atoi(r.FormValue("request_id"))
	if err != nil {
		ErrorHandler(w, "Invalid request ID", http.StatusBadRequest)
		return
	}

	// Обрабатываем заявку
	db, release, err := requestDB(r)
	if err != nil {
		ErrorHandler(w, "Database connection error", http.StatusInternalServerError)
		return
	}
	defer release()
	role, err := GetUserRole(db, UserID)
	if err != nil {
		ErrorHandler(w, "Error fetching user role", http.StatusInternalServerError)
		return
	}
	if role != "admin" {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	err = database.ApprovePromotionRequest(db, requestID)
	if err != nil {
		ErrorHandler(w, "Error approving request", http.StatusInternalServerError)
		return
	}

	// Перенаправляем обратно на страницу заявок
	http.Redirect(w, r, "/requests", http.StatusFound)
}

func DenyRequestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		ErrorHandler(w, "Method not allowed", 405)
		return
	}
	// Проверяем, что пользователь авторизован как админ
	sessionID, err := GetSessionID(w, r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}

	sessionUser, loggedIn := lookupSessionRequest(r, sessionID.Value)
	UserID := sessionUser.ID
	if !loggedIn {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	// Извлекаем ID заявки
	requestID, err := strconv.Atoi(r.FormValue("request_id"))
	if err != nil {
		ErrorHandler(w, "Invalid request ID", http.StatusBadRequest)
		return
	}

	// Обрабатываем заявку
	db, release, err := requestDB(r)
	if err != nil {
		ErrorHandler(w, "Database connection error", http.StatusInternalServerError)
		return
	}
	defer release()

	role, err := GetUserRole(db, UserID)
	if err != nil {
		ErrorHandler(w, "Error fetching user role", http.StatusInternalServerError)
		return
	}
	if role != "admin" {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}

	err = database.DenyPromotionRequest(db, requestID)
	if err != nil {
		ErrorHandler(w, "Error denying request", http.StatusInternalServerError)
		return
	}

	// Перенаправляем обратно на страницу заявок
	http.Redirect(w, r, "/requests", http.StatusFound)
}

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

// PostHandler обрабатывает отображение конкретного поста
func PostHandler(w http.ResponseWriter, r *http.Request) {
	// Проверка сессии
	sessionID, err := GetSessionID(w, r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}

	sessionUser, loggedIn := lookupSessionRequest(r, sessionID.Value)
	username := sessionUser.Initial

	// Получаем ID пользователя по имени
	UserID := sessionUser.ID
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
	// Получаем ID поста из параметров URL
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

	// Открываем соединение с базой данных
	db, release, err := requestDB(r)
	if err != nil {
		ErrorHandler(w, "Database connection error", http.StatusInternalServerError)
		return
	}
	defer release()

	// Получаем пост из базы данных
	post, err := database.GetPostByID(db, postID, UserID)
	if err != nil {
		ErrorHandler(w, "Post not found", http.StatusNotFound)
		return
	}

	// Получаем комментарии для поста
	comments := post.Comments

	// Проверяем, является ли текущий пользователь автором поста
	creator := post.AuthorID == UserID

	if r.Method == http.MethodPost && !loggedIn {
		ErrorHandler(w, "Sign in to perform this action", 401)
		return
	}
	if r.Method == http.MethodPost && r.URL.Query().Get("action") == "delete" && !creator && !Moders {
		ErrorHandler(w, "You cannot delete this post", 403)
		return
	}
	// Обрабатываем POST-запрос для добавления комментария
	if r.Method == http.MethodPost && loggedIn {

		if err := handlePostActions(w, r, db, postID, UserID, Moders); err != nil {
			return
		}

		if isReactionRequest(r) {
			state, err := database.ReactionState(db, "post", postID, UserID)
			if err != nil {
				ErrorHandler(w, "Unable to read reaction", 500)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(state)
			return
		}
		// Deleted posts redirect to the feed, other actions return to the post.
		target := "/post?id=" + strconv.Itoa(postID)
		if r.URL.Query().Get("action") == "delete" {
			target = "/"
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
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
	// Загружаем шаблон и передаем данные
	tmpl, err := parsePage("post.html")
	if err != nil {
		ErrorHandler(w, "Template parsing error", http.StatusInternalServerError)
		return
	}
	data := map[string]interface{}{
		"LoggedIn":      loggedIn,
		"Creator":       creator,
		"Username":      username,
		"Post":          post,
		"Comments":      comments,
		"SessionUserID": UserID,
		"Moder":         Moders,
		"Admin":         admin,
		"UserRole":      role,
		"Stat":          stat,
	}

	err = executeTemplate(w, tmpl, data)
	if err != nil {
		ErrorHandler(w, "Error rendering template", http.StatusInternalServerError)
		return
	}
}

// Обрабатываем лайки, дизлайки и удаление
func handlePostActions(w http.ResponseWriter, r *http.Request, db *sql.DB, postID int, userID int, moders bool) error {
	action := r.URL.Query().Get("action")
	switch action {
	case "like":
		if err := database.ToggleLike(db, postID, userID); err != nil {
			ErrorHandler(w, "Error processing like", http.StatusInternalServerError)
			return err
		}
	case "dislike":
		if err := database.ToggleDislike(db, postID, userID); err != nil {
			ErrorHandler(w, "Error processing dislike", http.StatusInternalServerError)
			return err
		}
	case "delete":
		if err := database.DeletePost(db, postID); err != nil {
			ErrorHandler(w, "Error deleting post", http.StatusInternalServerError)
			return err
		}
		database.DeletePostReport(db, postID)

	case "comment":
		commentText := r.FormValue("content")
		if commentText == "" {
			ErrorHandler(w, "Comment text is required", http.StatusBadRequest)
			return errors.New("invalid action or content")
		}
		if err := database.AddComment(db, postID, userID, commentText); err != nil {
			ErrorHandler(w, "Error adding comment", http.StatusInternalServerError)
			return err
		}
	case "report":
		if !moders {
			ErrorHandler(w, "Only moderators can report posts", http.StatusForbidden)
			return errors.New("forbidden")
		}
		err := database.ReportPost(db, postID, userID)
		if err != nil {
			ErrorHandler(w, "Database error", http.StatusInternalServerError)
			log.Println("Error updating report:", err)
			return err
		}
	default:
		ErrorHandler(w, "Invalid action", http.StatusBadRequest)
		return errors.New("invalid action or content")
	}

	return nil
}

func CommentHandler(w http.ResponseWriter, r *http.Request) {
	// Проверка сессии
	sessionID, err := GetSessionID(w, r)
	if err != nil {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}

	sessionUser, loggedIn := lookupSessionRequest(r, sessionID.Value)

	// Получаем ID пользователя по имени
	UserID := sessionUser.ID

	// Получаем ID поста из параметров URL
	commentIDstr := r.URL.Query().Get("id")
	if commentIDstr == "" {
		ErrorHandler(w, "Post ID is required", http.StatusBadRequest)
		return
	}

	commentID, err := strconv.Atoi(commentIDstr)
	if err != nil {
		ErrorHandler(w, "Invalid post ID", http.StatusBadRequest)
		return
	}

	// Открываем соединение с базой данных
	db, release, err := requestDB(r)
	if err != nil {
		ErrorHandler(w, "Database connection error", http.StatusInternalServerError)
		return
	}
	defer release()
	postID, err := database.GetPostIDByCommentID(db, commentID)
	if err != nil {
		ErrorHandler(w, "Comment not found", 404)
		return
	}
	if r.Method != http.MethodPost {
		ErrorHandler(w, "Method not allowed", 405)
		return
	}
	if !loggedIn {
		ErrorHandler(w, "Sign in to perform this action", 401)
		return
	}
	action := r.URL.Query().Get("action")
	if action == "delete" || action == "update" {
		var owner int
		if err := db.QueryRow("SELECT user_id FROM comments WHERE id=?", commentID).Scan(&owner); err != nil {
			ErrorHandler(w, "Comment not found", 404)
			return
		}
		role, err := GetUserRole(db, UserID)
		if err != nil {
			ErrorHandler(w, "Unable to check permissions", 500)
			return
		}
		if owner != UserID && (action == "update" || (role != "moderator" && role != "admin")) {
			ErrorHandler(w, "You cannot change this comment", 403)
			return
		}
	}
	if r.Method == http.MethodPost && loggedIn {

		if err := handleCommentActions(w, r, db, commentID, UserID); err != nil {
			return
		}

		// Перенаправляем на страницу поста с добавленным комментарием
		if isReactionRequest(r) {
			state, err := database.ReactionState(db, "comment", commentID, UserID)
			if err != nil {
				ErrorHandler(w, "Unable to read reaction", 500)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(state)
			return
		}
		http.Redirect(w, r, "/post?id="+strconv.Itoa(postID)+"#comment-"+strconv.Itoa(commentID), http.StatusSeeOther)
		return
	}
}

func isReactionRequest(r *http.Request) bool {
	action := r.URL.Query().Get("action")
	return (action == "like" || action == "dislike") && r.Header.Get("Accept") == "application/json"
}

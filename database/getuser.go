package database

import (
	"database/sql"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// User структура для пользователя
type User struct {
	ID           int
	Email        string
	Username     string
	PasswordHash string
}

// GetUserByEmail получает пользователя из базы данных по email
func GetUserByEmail(db *sql.DB, email string) (*User, error) {
	row := db.QueryRow("SELECT id, email, username, password FROM users WHERE LOWER(email) = LOWER(?)", email)
	user := &User{}
	err := row.Scan(&user.ID, &user.Email, &user.Username, &user.PasswordHash)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // Пользователь не найден
		}
		return nil, fmt.Errorf("ошибка при получении пользователя: %w", err)
	}
	return user, nil
}

// ComparePassword сравнивает введенный пароль с хешем пароля из базы данных
func ComparePassword(hashedPassword string, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
}

func CheckUsernameExists(username string) (bool, error) {
	db, err := InitDB()
	if err != nil {
		return false, err
	}
	defer db.Close()

	var count int
	err = db.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", username).Scan(&count)
	return count > 0, err
}

func GetPromotionStatus(db *sql.DB, userID int) (string, error) {
	var status string
	err := db.QueryRow("SELECT status FROM promotion_requests WHERE user_id = ?", userID).Scan(&status)
	if err != nil {
		if err == sql.ErrNoRows {
			return "no_request", nil // Если заявки нет, возвращаем специальное значение
		}
		return "", err // Возвращаем ошибку, если другая проблема
	}
	return status, nil
}

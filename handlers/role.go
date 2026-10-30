package handlers

import (
	"database/sql"
	"fmt"
	"log"
)

func GetUserRole(db *sql.DB, userID int) (string, error) {
	var role string
	query := `SELECT role FROM users WHERE id = ?`

	err := db.QueryRow(query, userID).Scan(&role)
	if err != nil {
		if err == sql.ErrNoRows {
			// Если пользователь не найден, возвращаем ошибку
			return "", fmt.Errorf("user with ID %d not found", userID)
		}
		// Если произошла другая ошибка
		log.Println("Error fetching user role:", err)
		return "", err
	}

	return role, nil
}

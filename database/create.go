package database

import (
	"database/sql"
	"fmt"
	"log"

	"golang.org/x/crypto/bcrypt"
)

// hashPassword is a function that would hash the password before saving it
func hashPassword(password string) (string, error) {
	// For example, using bcrypt:
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("error hashing password: %w", err)
	}
	return string(hashedPassword), nil
}

// CreateCategory создает новую категорию
func CreateCategory(db *sql.DB, name string) error {
	_, err := db.Exec(`INSERT INTO categories (name) VALUES (?)`, name)
	return err
}

// CreatePost creates a post and assigns its categories in the same transaction.
func CreatePost(db *sql.DB, title, content string, authorID int, categoryIDs []int, createdAt, imagePath string) error {
	if err := ValidatePost(title, content); err != nil {
		return err
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var legacyCategoryID interface{}
	if len(categoryIDs) > 0 {
		legacyCategoryID = categoryIDs[0]
	}
	result, err := tx.Exec("INSERT INTO posts(title,content,author_id,category_id,created_at,image_path) VALUES(?,?,?,?,?,?)", title, content, authorID, legacyCategoryID, createdAt, imagePath)
	if err != nil {
		return err
	}
	postID, err := result.LastInsertId()
	if err != nil {
		return err
	}
	for _, categoryID := range categoryIDs {
		if _, err = tx.Exec("INSERT INTO post_categories(post_id,category_id) VALUES(?,?)", postID, categoryID); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("INSERT INTO activities(user_id,activity_type,post_id) VALUES(?,'create_post',?)", authorID, postID); err != nil {
		return err
	}
	return tx.Commit()
}
func CreateUser(db *sql.DB, email, username, password, role string) error {
	// Validate the role before inserting
	validRoles := map[string]bool{
		"guest":     true,
		"user":      true,
		"moderator": true,
		"admin":     true,
	}

	if !validRoles[role] {
		return fmt.Errorf("invalid role: %s", role)
	}

	// Hash the password (consider using bcrypt or Argon2)
	hashedPassword, err := hashPassword(password)
	if err != nil {
		return fmt.Errorf("error hashing password: %w", err)
	}

	query := `INSERT INTO users (email, username, password, role) VALUES (?, ?, ?, ?);`

	// Prepare the query
	stmt, err := db.Prepare(query)
	if err != nil {
		return fmt.Errorf("error preparing statement: %w", err)
	}
	defer stmt.Close()

	// Execute the query
	_, err = stmt.Exec(email, username, hashedPassword, role)
	if err != nil {
		return fmt.Errorf("error creating user: %w", err)
	}

	return nil
}

func ReadNotification(db *sql.DB, ID int) {
	fmt.Println(ID)
	_, err := db.Exec("UPDATE notifications SET is_read = 1 WHERE id = ?", ID)
	if err != nil {
		log.Println(err)
	}
}

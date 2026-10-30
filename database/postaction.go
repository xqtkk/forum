package database

import (
	"database/sql"
	"fmt"
	"strings"
)

func EditPost(db *sql.DB, title, content string, categoryIDs []int, updatedAt string, id int, imagePath string) error {
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
	if _, err = tx.Exec("UPDATE posts SET title=?,content=?,category_id=?,updated_at=?,image_path=? WHERE id=?", title, content, legacyCategoryID, updatedAt, imagePath, id); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM post_categories WHERE post_id=?", id); err != nil {
		return err
	}
	for _, categoryID := range categoryIDs {
		if _, err = tx.Exec("INSERT INTO post_categories(post_id,category_id) VALUES(?,?)", id, categoryID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func ValidatePost(title, content string) error {
	if len([]rune(strings.TrimSpace(title))) < 1 || len([]rune(title)) > 200 || len([]rune(strings.TrimSpace(content))) < 1 || len([]rune(content)) > 20000 {
		return fmt.Errorf("title must be 1–200 characters and content 1–20,000 characters")
	}
	return nil
}
func ToggleLike(db *sql.DB, postID, userID int) error {
	return toggleReaction(db, "post", postID, userID, "like")
}
func ToggleDislike(db *sql.DB, postID, userID int) error {
	return toggleReaction(db, "post", postID, userID, "dislike")
}
func DeletePost(db *sql.DB, postID int) error {
	_, err := db.Exec("DELETE FROM posts WHERE id=?", postID)
	return err
}
func AddComment(db *sql.DB, postID, userID int, content string) error {
	content = strings.TrimSpace(content)
	if len([]rune(content)) < 1 || len([]rune(content)) > 5000 {
		return fmt.Errorf("comment must contain 1–5,000 characters")
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner int
	if err = tx.QueryRow("SELECT author_id FROM posts WHERE id=?", postID).Scan(&owner); err != nil {
		return err
	}
	result, err := tx.Exec("INSERT INTO comments(post_id,user_id,content) VALUES(?,?,?)", postID, userID, content)
	if err != nil {
		return err
	}
	commentID, err := result.LastInsertId()
	if err != nil {
		return err
	}
	if _, err = tx.Exec("INSERT INTO activities(user_id,activity_type,post_id,comment_id,comment_content) VALUES(?,'You commented',?,?,?)", userID, postID, commentID, content); err != nil {
		return err
	}
	if owner != userID {
		if _, err = tx.Exec("INSERT INTO notifications(user_id,notification_type,post_id,comment_id,comment_content) VALUES(?,'You received a comment',?,?,?)", owner, postID, commentID, content); err != nil {
			return err
		}
	}
	return tx.Commit()
}

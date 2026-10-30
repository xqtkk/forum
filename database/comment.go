package database

import (
	"database/sql"
	"fmt"
	"strings"
)

func ToggleDislikeComment(db *sql.DB, id, userID int) error {
	return toggleReaction(db, "comment", id, userID, "dislike")
}
func ToggleLikeComment(db *sql.DB, id, userID int) error {
	return toggleReaction(db, "comment", id, userID, "like")
}
func DeleteComment(db *sql.DB, id int) error {
	_, err := db.Exec("DELETE FROM comments WHERE id=?", id)
	return err
}
func UpdateComment(db *sql.DB, id, userID int, content string) error {
	content = strings.TrimSpace(content)
	if len([]rune(content)) < 1 || len([]rune(content)) > 5000 {
		return fmt.Errorf("comment must contain 1–5,000 characters")
	}
	result, err := db.Exec("UPDATE comments SET content=?,updated_at=CURRENT_TIMESTAMP WHERE id=? AND user_id=?", content, id, userID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if n == 0 && err == nil {
		return fmt.Errorf("comment not found or permission denied")
	}
	return err
}

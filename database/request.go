package database

import (
	"database/sql"
	"fmt"
	"time"
)

type PromotionRequest struct {
	ID       int
	UserID   int
	Username string
	Reason   string
	Created  time.Time
	Status   string
}

// GetPendingPromotionRequests получает все заявки на повышение с статусом 'pending'
func GetPendingPromotionRequests(db *sql.DB) ([]PromotionRequest, error) {
	rows, err := db.Query(`
		SELECT pr.id, pr.user_id, u.username, pr.reason, pr.request_date, pr.status 
		FROM promotion_requests pr
		JOIN users u ON pr.user_id = u.id
		WHERE pr.status = 'pending'`)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch pending promotion requests: %v", err)
	}
	defer rows.Close()

	var requests []PromotionRequest
	for rows.Next() {
		var req PromotionRequest
		err := rows.Scan(&req.ID, &req.UserID, &req.Username, &req.Reason, &req.Created, &req.Status)
		if err != nil {
			return nil, fmt.Errorf("failed to scan row: %v", err)
		}
		requests = append(requests, req)
	}

	return requests, nil
}

// ApprovePromotionRequest обновляет статус заявки на 'approved'
func ApprovePromotionRequest(db *sql.DB, requestID int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var userID int
	if err = tx.QueryRow("SELECT user_id FROM promotion_requests WHERE id=? AND status='pending'", requestID).Scan(&userID); err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE promotion_requests SET status='approved' WHERE id=?", requestID); err != nil {
		return err
	}
	if _, err = tx.Exec("UPDATE users SET role='moderator' WHERE id=? AND role!='admin'", userID); err != nil {
		return err
	}
	return tx.Commit()
}

// DenyPromotionRequest обновляет статус заявки на 'denied'
func DenyPromotionRequest(db *sql.DB, requestID int) error {
	_, err := db.Exec(`UPDATE promotion_requests SET status = 'denied' WHERE id = ?`, requestID)
	return err
}

func ReportPost(db *sql.DB, postID, userID int) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var existing int
	if err = tx.QueryRow("SELECT COUNT(*) FROM reports WHERE post_id=? AND reported_by=?", postID, userID).Scan(&existing); err != nil {
		return err
	}
	if existing == 0 {
		_, err = tx.Exec("INSERT INTO reports(post_id,reported_by,status) VALUES(?,?,'open')", postID, userID)
	} else {
		_, err = tx.Exec("UPDATE reports SET status='open' WHERE post_id=? AND reported_by=?", postID, userID)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}

func DeletePostReport(db *sql.DB, postID int) error {
	// Обновляем статус репорта, если он существует
	_, err := db.Exec("UPDATE reports SET status = 'none' WHERE post_id = ?", postID)
	if err != nil {
		return err
	}

	return nil
}

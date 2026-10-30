package serverconfig

import (
	"context"
	"database/sql"
	"fmt"
	"forum/database"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func DailyBackup(db *sql.DB) error {
	dir := os.Getenv("FORUM_BACKUP_DIR")
	if dir == "" {
		dir = ".local/backups"
	}
	today := filepath.Join(dir, "forum-"+time.Now().UTC().Format("20060102")+".db")
	if _, err := os.Stat(today); os.IsNotExist(err) {
		if err = database.Backup(db, today); err != nil {
			return fmt.Errorf("database backup: %w", err)
		}
	} else if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -7)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasPrefix(entry.Name(), "forum-") || !strings.HasSuffix(entry.Name(), ".db") {
			continue
		}
		date, err := time.Parse("20060102", strings.TrimSuffix(strings.TrimPrefix(entry.Name(), "forum-"), ".db"))
		if err == nil && date.Before(cutoff) {
			if err = os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				return err
			}
		}
	}
	return nil
}
func Maintain(ctx context.Context, db *sql.DB) {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for _, query := range []string{"DELETE FROM sessions WHERE expires_at<=CURRENT_TIMESTAMP", "DELETE FROM account_tokens WHERE expires_at<=CURRENT_TIMESTAMP", "DELETE FROM oauth_flows WHERE expires_at<=CURRENT_TIMESTAMP", "DELETE FROM login_attempts WHERE updated_at<datetime('now','-1 day')"} {
				if _, err := db.ExecContext(ctx, query); err != nil {
					log.Printf("maintenance failed: %v", err)
				}
			}
			if err := DailyBackup(db); err != nil {
				log.Printf("scheduled backup failed: %v", err)
			}
		}
	}
}

package database

import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

func DatabasePath() string {
	p := os.Getenv("FORUM_DB_PATH")
	if p == "" {
		p = "./forum.db"
	}
	return p
}
func OpenDB() (*sql.DB, error) {
	path, err := filepath.Abs(DatabasePath())
	if err != nil {
		return nil, err
	}
	path = filepath.ToSlash(path)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u := url.URL{Scheme: "file", Path: path}
	db, err := sql.Open("sqlite3", u.String()+"?_busy_timeout=10000&_foreign_keys=on&_journal_mode=WAL&_synchronous=FULL&_txlock=immediate")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(8)
	if err = db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
func InitDB() (*sql.DB, error) {
	db, err := OpenDB()
	if err != nil {
		return nil, err
	}
	if err = migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
func CheckEmailExists(email string) (bool, error) {
	db, err := OpenDB()
	if err != nil {
		return false, err
	}
	defer db.Close()
	var n int
	err = db.QueryRow("SELECT COUNT(*) FROM users WHERE LOWER(email)=LOWER(?)", email).Scan(&n)
	return n > 0, err
}

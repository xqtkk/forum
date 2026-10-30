package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var baseSchema = []struct{ name, sql string }{
	{"oauth_accounts", `CREATE TABLE IF NOT EXISTS oauth_accounts (provider TEXT NOT NULL, subject TEXT NOT NULL, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, PRIMARY KEY(provider,subject));`},
	{"users", `CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			email TEXT UNIQUE NOT NULL,
			username TEXT UNIQUE NOT NULL,
			password TEXT NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('guest', 'user', 'moderator', 'admin')),
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);`},
	{"categories", `CREATE TABLE IF NOT EXISTS categories (
			id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL UNIQUE
		);`},
	{"posts", `CREATE TABLE IF NOT EXISTS posts (
			id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			content TEXT NOT NULL,
			category_id INTEGER,
			author_id INTEGER NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP,
			liked INTEGER DEFAULT 0, 
			disliked INTEGER DEFAULT 0, 
			image_path TEXT, 
			FOREIGN KEY (category_id) REFERENCES categories(id) ON DELETE SET NULL,
			FOREIGN KEY (author_id) REFERENCES users(id) ON DELETE CASCADE
		);`},
	{"sessions", `CREATE TABLE IF NOT EXISTS sessions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			session_id TEXT UNIQUE NOT NULL,
			user_id INTEGER NOT NULL,
			expires_at TIMESTAMP NOT NULL,
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
		);`},
	{"promotion_requests", `CREATE TABLE IF NOT EXISTS promotion_requests (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_id INTEGER NOT NULL,
			reason TEXT NOT NULL,
			request_date DATETIME DEFAULT CURRENT_TIMESTAMP,
			status TEXT NOT NULL CHECK(status IN ('pending', 'approved', 'denied')),
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);`},
	{"post_reactions", `CREATE TABLE IF NOT EXISTS post_reactions (
			post_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			reaction_type TEXT CHECK(reaction_type IN ('like', 'dislike')) NOT NULL, 
			PRIMARY KEY (post_id, user_id),
			FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);`},
	{"comment_reactions", `CREATE TABLE IF NOT EXISTS comment_reactions (
			comment_id INTEGER NOT NULL,
			user_id INTEGER NOT NULL,
			reaction_type TEXT CHECK(reaction_type IN ('like', 'dislike')) NOT NULL, 
			PRIMARY KEY (comment_id, user_id),
			FOREIGN KEY (comment_id) REFERENCES comments(id) ON DELETE CASCADE,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
		);`},
	{"comments", `CREATE TABLE IF NOT EXISTS comments (
			id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
			post_id INTEGER NOT NULL,            
			user_id INTEGER NOT NULL,            
			content TEXT NOT NULL,               
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,  
			updated_at TIMESTAMP,
			liked INTEGER DEFAULT 0, 
			disliked INTEGER DEFAULT 0, 
			FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE,      
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE       
		);`},
	{"reports", `CREATE TABLE IF NOT EXISTS reports (
			id INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
			post_id INTEGER,
			reported_by INTEGER NOT NULL,
			status TEXT DEFAULT 'none' CHECK(status IN ('none','open', 'resolved')),
			FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE CASCADE,
			FOREIGN KEY (reported_by) REFERENCES users(id) ON DELETE CASCADE
		);`},
	{"activities", `CREATE TABLE IF NOT EXISTS activities (
			id INTEGER PRIMARY KEY,
			user_id INTEGER,
			activity_type VARCHAR(20) NOT NULL, 
			post_id INTEGER,                        
			comment_id INTEGER,                     
			comment_content TEXT DEFAULT NULL, 
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE SET NULL,
			FOREIGN KEY (comment_id) REFERENCES comments(id) ON DELETE SET NULL
		);`},
	{"notifications", `CREATE TABLE IF NOT EXISTS notifications (
			id INTEGER PRIMARY KEY,
			user_id INTEGER,
			notification_type TEXT NOT NULL, 						
			post_id INTEGER,    
			comment_id INTEGER,                      				
			comment_content TEXT DEFAULT NULL,                      
			is_read BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY (post_id) REFERENCES posts(id) ON DELETE SET NULL
		, FOREIGN KEY(comment_id) REFERENCES comments(id) ON DELETE SET NULL
);`},
}

// Version 1 repairs legacy references and installs explicit delete behavior.
// It runs on one dedicated connection with FK checks restored before release.
func migrate(db *sql.DB) error {
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		return err
	}
	if version > 4 {
		return fmt.Errorf("database schema %d is newer than this application", version)
	}
	if version == 4 {
		return CheckIntegrity(db)
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.ExecContext(context.Background(), "PRAGMA foreign_keys=OFF"); err != nil {
		return err
	}
	defer conn.ExecContext(context.Background(), "PRAGMA foreign_keys=ON")
	var existing int
	if err = conn.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='users'").Scan(&existing); err != nil {
		return err
	}
	if existing > 0 {
		backup := DatabasePath() + ".pre-v2-" + time.Now().UTC().Format("20060102T150405.000000000") + ".bak"
		if _, err = conn.ExecContext(context.Background(), "VACUUM INTO ?", backup); err != nil {
			return fmt.Errorf("migration backup failed: %w", err)
		}
	}
	tx, err := conn.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if version < 1 {
		for _, table := range baseSchema {
			if _, err = tx.Exec(table.sql); err != nil {
				return err
			}
		}
		if _, err = tx.Exec(`CREATE TABLE IF NOT EXISTS migration_orphans(id INTEGER PRIMARY KEY,table_name TEXT NOT NULL,payload TEXT NOT NULL,archived_at TEXT DEFAULT CURRENT_TIMESTAMP)`); err != nil {
			return err
		}
		repairs := []struct{ table, predicate string }{
			{"posts", "author_id NOT IN(SELECT id FROM users)"}, {"comments", "post_id NOT IN(SELECT id FROM posts) OR user_id NOT IN(SELECT id FROM users)"},
			{"sessions", "user_id NOT IN(SELECT id FROM users)"}, {"promotion_requests", "user_id NOT IN(SELECT id FROM users)"},
			{"oauth_accounts", "user_id NOT IN(SELECT id FROM users)"},
			{"post_reactions", "post_id NOT IN(SELECT id FROM posts) OR user_id NOT IN(SELECT id FROM users)"},
			{"comment_reactions", "comment_id NOT IN(SELECT id FROM comments) OR user_id NOT IN(SELECT id FROM users)"},
			{"reports", "post_id NOT IN(SELECT id FROM posts) OR reported_by NOT IN(SELECT id FROM users)"},
			{"activities", "user_id IS NULL OR user_id NOT IN(SELECT id FROM users)"}, {"notifications", "user_id IS NULL OR user_id NOT IN(SELECT id FROM users)"},
		}
		for _, repair := range repairs {
			if err = archiveInvalid(tx, repair.table, repair.predicate); err != nil {
				return err
			}
		}
		for _, q := range []string{
			"UPDATE posts SET category_id=NULL WHERE category_id NOT IN(SELECT id FROM categories)",
			"UPDATE activities SET post_id=NULL WHERE post_id NOT IN(SELECT id FROM posts)", "UPDATE activities SET comment_id=NULL WHERE comment_id NOT IN(SELECT id FROM comments)",
			"UPDATE notifications SET post_id=NULL WHERE post_id NOT IN(SELECT id FROM posts)", "UPDATE notifications SET comment_id=NULL WHERE comment_id NOT IN(SELECT id FROM comments)",
		} {
			if _, err = tx.Exec(q); err != nil {
				return err
			}
		}
		for _, table := range baseSchema {
			rows, e := tx.Query("PRAGMA table_info(" + table.name + ")")
			if e != nil {
				return e
			}
			var columns []string
			for rows.Next() {
				var cid, notnull, pk int
				var name, kind string
				var def interface{}
				if e = rows.Scan(&cid, &name, &kind, &notnull, &def, &pk); e != nil {
					rows.Close()
					return e
				}
				columns = append(columns, `"`+name+`"`)
			}
			if e = rows.Err(); e != nil {
				rows.Close()
				return e
			}
			rows.Close()
			create := strings.Replace(table.sql, "IF NOT EXISTS "+table.name, "IF NOT EXISTS "+table.name+"_new", 1)
			if _, err = tx.Exec(create); err != nil {
				return err
			}
			fields := strings.Join(columns, ",")
			if _, err = tx.Exec("INSERT INTO " + table.name + "_new(" + fields + ") SELECT " + fields + " FROM " + table.name); err != nil {
				return err
			}
			if _, err = tx.Exec("DROP TABLE " + table.name); err != nil {
				return err
			}
			if _, err = tx.Exec("ALTER TABLE " + table.name + "_new RENAME TO " + table.name); err != nil {
				return err
			}
		}
	}
	if version < 2 {
		for _, q := range []string{
			"ALTER TABLE users ADD COLUMN bio TEXT NOT NULL DEFAULT ''",
			"ALTER TABLE users ADD COLUMN email_verified INTEGER NOT NULL DEFAULT 0",
			`CREATE TABLE account_tokens(token_hash TEXT PRIMARY KEY,user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,purpose TEXT NOT NULL CHECK(purpose IN('verify','reset')),expires_at TEXT NOT NULL)`,
			`CREATE TABLE oauth_flows(state_hash TEXT PRIMARY KEY,provider TEXT NOT NULL,verifier TEXT NOT NULL,expires_at TEXT NOT NULL,link_user_id INTEGER REFERENCES users(id) ON DELETE CASCADE,session_hash TEXT NOT NULL DEFAULT '')`,
			`CREATE TABLE login_attempts(account_hash TEXT PRIMARY KEY,failures INTEGER NOT NULL DEFAULT 0,blocked_until TEXT,updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
			"CREATE UNIQUE INDEX users_email_ci ON users(LOWER(email))",
			"CREATE INDEX sessions_expiry ON sessions(expires_at)", "CREATE INDEX sessions_user ON sessions(user_id)",
			"CREATE INDEX posts_feed ON posts(category_id,created_at DESC,id DESC)", "CREATE INDEX posts_author ON posts(author_id,created_at DESC,id DESC)",
			"CREATE INDEX comments_post ON comments(post_id,created_at,id)", "CREATE INDEX comments_author ON comments(user_id,created_at DESC,id DESC)",
			"CREATE INDEX notifications_user ON notifications(user_id,is_read,id DESC)", "CREATE INDEX activities_user ON activities(user_id,id DESC)",
			"CREATE INDEX account_tokens_expiry ON account_tokens(expires_at)", "CREATE INDEX oauth_flows_expiry ON oauth_flows(expires_at)",
			"UPDATE posts SET liked=(SELECT COUNT(*) FROM post_reactions r WHERE r.post_id=posts.id AND r.reaction_type='like'),disliked=(SELECT COUNT(*) FROM post_reactions r WHERE r.post_id=posts.id AND r.reaction_type='dislike')",
			"UPDATE comments SET liked=(SELECT COUNT(*) FROM comment_reactions r WHERE r.comment_id=comments.id AND r.reaction_type='like'),disliked=(SELECT COUNT(*) FROM comment_reactions r WHERE r.comment_id=comments.id AND r.reaction_type='dislike')",
		} {
			if _, err = tx.Exec(q); err != nil {
				return fmt.Errorf("migration: %w", err)
			}
		}
	}
	if version < 3 {
		if _, err = tx.Exec("UPDATE users SET email_verified=0 WHERE role='user' AND NOT EXISTS(SELECT 1 FROM oauth_accounts WHERE user_id=users.id)"); err != nil {
			return err
		}
	}
	if version < 4 {
		for _, q := range []string{
			`CREATE TABLE post_categories(post_id INTEGER NOT NULL REFERENCES posts(id) ON DELETE CASCADE,category_id INTEGER NOT NULL REFERENCES categories(id) ON DELETE CASCADE,PRIMARY KEY(post_id,category_id))`,
			`INSERT OR IGNORE INTO post_categories(post_id,category_id) SELECT id,category_id FROM posts WHERE category_id IS NOT NULL AND category_id IN(SELECT id FROM categories)`,
			`CREATE INDEX post_categories_category ON post_categories(category_id,post_id)`,
		} {
			if _, err = tx.Exec(q); err != nil {
				return fmt.Errorf("migration: %w", err)
			}
		}
	}
	rows, err := tx.Query("PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	invalid := rows.Next()
	rows.Close()
	if invalid {
		return fmt.Errorf("foreign key validation failed; migration rolled back")
	}
	if _, err = tx.Exec("PRAGMA user_version=4"); err != nil {
		return err
	}
	return tx.Commit()
}
func archiveInvalid(tx *sql.Tx, table, predicate string) error {
	rows, err := tx.Query("SELECT * FROM " + table + " WHERE " + predicate)
	if err != nil {
		return err
	}
	columns, err := rows.Columns()
	if err != nil {
		rows.Close()
		return err
	}
	var archive []string
	for rows.Next() {
		values := make([]interface{}, len(columns))
		ptr := make([]interface{}, len(columns))
		for i := range values {
			ptr[i] = &values[i]
		}
		if err = rows.Scan(ptr...); err != nil {
			rows.Close()
			return err
		}
		entry := map[string]interface{}{}
		for i, c := range columns {
			entry[c] = values[i]
		}
		b, e := json.Marshal(entry)
		if e != nil {
			rows.Close()
			return e
		}
		archive = append(archive, string(b))
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, payload := range archive {
		if _, err = tx.Exec("INSERT INTO migration_orphans(table_name,payload) VALUES(?,?)", table, payload); err != nil {
			return err
		}
	}
	_, err = tx.Exec("DELETE FROM " + table + " WHERE " + predicate)
	return err
}
func CheckIntegrity(db *sql.DB) error {
	rows, err := db.Query("PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("database has broken foreign keys")
	}
	return rows.Err()
}
func Backup(db *sql.DB, destination string) error {
	if _, err := os.Stat(destination); err == nil {
		return fmt.Errorf("backup destination already exists")
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	_, err := db.Exec("VACUUM INTO ?", destination)
	return err
}

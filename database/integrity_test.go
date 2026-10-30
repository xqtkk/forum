package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func freshDB(t *testing.T) *sql.DB {
	t.Helper()
	t.Setenv("FORUM_DB_PATH", filepath.Join(t.TempDir(), "forum.db"))
	db, err := InitDB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}
func TestLegacyMigrationBacksUpAndPreservesRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	t.Setenv("FORUM_DB_PATH", path)
	db, err := OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = conn.ExecContext(context.Background(), "PRAGMA foreign_keys=OFF"); err != nil {
		t.Fatal(err)
	}
	for _, table := range baseSchema {
		if _, err = conn.ExecContext(context.Background(), table.sql); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{"INSERT INTO users(id,email,username,password,role) VALUES(1,'legacy@example.test','legacy','!test','user')", "INSERT INTO posts(id,title,content,author_id,category_id,liked) VALUES(1,'Legacy','Kept',1,999,7)", "INSERT INTO comments(id,post_id,user_id,content) VALUES(1,1,1,'Kept comment')", "INSERT INTO comment_reactions(comment_id,user_id,reaction_type) VALUES(999,1,'like')"} {
		if _, err = conn.ExecContext(context.Background(), q); err != nil {
			t.Fatal(err)
		}
	}
	conn.Close()
	db.Close()
	db, err = InitDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = CheckIntegrity(db); err != nil {
		t.Fatal(err)
	}
	var content string
	var category sql.NullInt64
	var count, likes int
	if err = db.QueryRow("SELECT content,category_id,liked FROM posts WHERE id=1").Scan(&content, &category, &likes); err != nil {
		t.Fatal(err)
	}
	if content != "Kept" || category.Valid || likes != 0 {
		t.Fatal("legacy content or repaired state incorrect")
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM comments").Scan(&count); err != nil || count != 1 {
		t.Fatal("valid comment lost")
	}
	if err = db.QueryRow("SELECT COUNT(*) FROM migration_orphans WHERE table_name='comment_reactions'").Scan(&count); err != nil || count != 1 {
		t.Fatal("orphan was not archived")
	}
	backups, _ := filepath.Glob(path + ".pre-v2-*.bak")
	if len(backups) != 1 {
		t.Fatal("missing migration backup")
	}
	again, err := InitDB()
	if err != nil {
		t.Fatal(err)
	}
	again.Close()
}

func TestForeignKeysAndCascades(t *testing.T) {
	db := freshDB(t)
	for _, q := range []string{"INSERT INTO users(id,email,username,password,role) VALUES(1,'a@example.test','aaa','!test','user'),(2,'b@example.test','bbb','!test','user')", "INSERT INTO posts(id,title,content,author_id) VALUES(1,'Title','Content',1)", "INSERT INTO comments(id,post_id,user_id,content) VALUES(1,1,2,'Comment')", "INSERT INTO post_reactions VALUES(1,2,'like')", "INSERT INTO comment_reactions VALUES(1,1,'like')", "INSERT INTO notifications(user_id,notification_type,post_id,comment_id) VALUES(1,'Comment',1,1)", "INSERT INTO activities(user_id,activity_type,post_id,comment_id) VALUES(2,'Comment',1,1)"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("INSERT INTO comments(post_id,user_id,content) VALUES(999,1,'Invalid')"); err == nil {
		t.Fatal("foreign key enforcement disabled")
	}
	if err := DeletePost(db, 1); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"comments", "post_reactions", "comment_reactions"} {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil || n != 0 {
			t.Fatalf("cascade failed for %s", table)
		}
	}
	if err := CheckIntegrity(db); err != nil {
		t.Fatal(err)
	}
	var post, comment sql.NullInt64
	if err := db.QueryRow("SELECT post_id,comment_id FROM notifications").Scan(&post, &comment); err != nil || post.Valid || comment.Valid {
		t.Fatal("notification links not cleared")
	}
}

func TestBusinessOperationsRollbackTogether(t *testing.T) {
	db := freshDB(t)
	if _, err := db.Exec("INSERT INTO users(id,email,username,password,role) VALUES(1,'a@example.test','aaa','!test','user'); INSERT INTO posts(id,title,content,author_id) VALUES(1,'Title','Content',1); CREATE TRIGGER reject_activity BEFORE INSERT ON activities BEGIN SELECT RAISE(ABORT,'forced failure'); END;"); err != nil {
		t.Fatal(err)
	}
	if err := ToggleLike(db, 1, 1); err == nil {
		t.Fatal("reaction ignored activity error")
	}
	if err := AddComment(db, 1, 1, "Should rollback"); err == nil {
		t.Fatal("comment ignored activity error")
	}
	if err := CreatePost(db, "Should rollback", "Content", 1, nil, "2026-10-29 00:00:00", ""); err == nil {
		t.Fatal("post ignored activity error")
	}
	for _, table := range []string{"post_reactions", "comments"} {
		var n int
		db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n)
		if n != 0 {
			t.Fatalf("partial write in %s", table)
		}
	}
	var likes, posts int
	db.QueryRow("SELECT liked FROM posts WHERE id=1").Scan(&likes)
	db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&posts)
	if likes != 0 || posts != 1 {
		t.Fatal("partial counters or post insert")
	}
}

func TestConcurrentReactionsMatchCounters(t *testing.T) {
	db := freshDB(t)
	if _, err := db.Exec("INSERT INTO users(id,email,username,password,role) VALUES(1,'a@example.test','aaa','!test','user'); INSERT INTO posts(id,title,content,author_id) VALUES(1,'Title','Content',1)"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	failures := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := ToggleLike(db, 1, 1); err != nil {
				failures <- err
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	state, err := ReactionState(db, "post", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if state.Likes != 0 || state.Dislikes != 0 || state.Reaction != "" {
		t.Fatalf("inconsistent state: %+v", state)
	}
}

func TestMigrationRejectsDuplicateEmailWithoutPartialWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	t.Setenv("FORUM_DB_PATH", path)
	db, err := OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	for _, table := range baseSchema {
		if _, err = db.Exec(table.sql); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = db.Exec("INSERT INTO users(email,username,password,role) VALUES('same@example.test','first','!test','user'),('SAME@example.test','second','!test','user')"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err = InitDB(); err == nil || !strings.Contains(err.Error(), "UNIQUE") {
		t.Fatalf("duplicate legacy emails not detected: %v", err)
	}
	db, err = OpenDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var count, version int
	db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	db.QueryRow("PRAGMA user_version").Scan(&version)
	if count != 2 || version != 0 {
		t.Fatal("failed migration changed legacy records")
	}
}

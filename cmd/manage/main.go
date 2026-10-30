package main

import (
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"forum/database"
	"golang.org/x/crypto/bcrypt"
	"log"
	"os"
)

func main() {
	create := flag.Bool("create-moderator", false, "Create a moderator with a unique generated password")
	createAdmin := flag.Bool("create-admin", false, "Create an administrator with a unique generated password")
	backup := flag.String("backup", "", "Write a consistent SQLite backup to a new destination")
	check := flag.Bool("check", false, "Validate database integrity")
	quarantine := flag.Bool("quarantine-legacy", false, "Disable legacy shared OAuth passwords; recover by verified email")
	flag.Parse()
	if !*create && !*createAdmin && !*check && !*quarantine && *backup == "" {
		flag.Usage()
		return
	}
	db, err := database.InitDB()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if *backup != "" {
		if err = database.Backup(db, *backup); err != nil {
			log.Fatal(err)
		}
		fmt.Println("Backup created:", *backup)
	}
	if *check {
		if err = database.CheckIntegrity(db); err != nil {
			log.Fatal(err)
		}
		var result string
		if err = db.QueryRow("PRAGMA integrity_check").Scan(&result); err != nil || result != "ok" {
			log.Fatalf("integrity check failed: %s %v", result, err)
		}
		fmt.Println("Database integrity: ok")
	}
	if *quarantine {
		rows, err := db.Query("SELECT id,password FROM users WHERE password LIKE '$2%'")
		if err != nil {
			log.Fatal(err)
		}
		var ids []int
		for rows.Next() {
			var id int
			var password string
			if err = rows.Scan(&id, &password); err != nil {
				log.Fatal(err)
			}
			if bcrypt.CompareHashAndPassword([]byte(password), []byte("defaultpassword")) == nil {
				ids = append(ids, id)
			}
		}
		if err = rows.Err(); err != nil {
			log.Fatal(err)
		}
		rows.Close()
		tx, err := db.Begin()
		if err != nil {
			log.Fatal(err)
		}
		defer tx.Rollback()
		for _, id := range ids {
			if _, err = tx.Exec("UPDATE users SET password='!legacy-recovery',email_verified=0 WHERE id=?", id); err != nil {
				log.Fatal(err)
			}
			if _, err = tx.Exec("DELETE FROM sessions WHERE user_id=?", id); err != nil {
				log.Fatal(err)
			}
		}
		if err = tx.Commit(); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Legacy shared passwords disabled: %d\n", len(ids))
	}
	if *create || *createAdmin {
		role := "moderator"
		name, email := "forum_moderator", "moderator@forum.local"
		if *createAdmin {
			role = "admin"
			name, email = "forum_admin", "admin@forum.local"
		}
		for suffix := 1; ; suffix++ {
			var count int
			if err = db.QueryRow("SELECT COUNT(*) FROM users WHERE username=? OR LOWER(email)=LOWER(?)", name, email).Scan(&count); err != nil {
				log.Fatal(err)
			}
			if count == 0 {
				break
			}
			if *createAdmin {
				name = fmt.Sprintf("forum_admin_%d", suffix)
				email = fmt.Sprintf("admin%d@forum.local", suffix)
			} else {
				name = fmt.Sprintf("forum_moderator_%d", suffix)
				email = fmt.Sprintf("moderator%d@forum.local", suffix)
			}
		}
		entropy := make([]byte, 18)
		if _, err = rand.Read(entropy); err != nil {
			log.Fatal(err)
		}
		password := "M!9a" + base64.RawURLEncoding.EncodeToString(entropy)
		if err = database.CreateUser(db, email, name, password, role); err != nil {
			log.Fatal(err)
		}
		fmt.Fprintf(os.Stdout, "%s created\nUsername: %s\nEmail: %s\nPassword: %s\n", role, name, email, password)
	}
}

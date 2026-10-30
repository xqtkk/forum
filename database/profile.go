package database

import (
	"database/sql"
	"fmt"
)

type Profile struct {
	ID                          int
	Username, Bio, Role, Joined string
	PostCount, CommentCount     int
}
type ProfileComment struct {
	ID, PostID                    int
	Content, PostTitle, CreatedAt string
}

func GetProfile(db *sql.DB, id int) (Profile, error) {
	var p Profile
	err := db.QueryRow(`SELECT id,username,bio,role,CAST(created_at AS TEXT),(SELECT COUNT(*) FROM posts WHERE author_id=u.id),(SELECT COUNT(*) FROM comments WHERE user_id=u.id) FROM users u WHERE id=?`, id).Scan(&p.ID, &p.Username, &p.Bio, &p.Role, &p.Joined, &p.PostCount, &p.CommentCount)
	return p, err
}
func ProfilePosts(db *sql.DB, id, page int) ([]Post, error) {
	rows, err := db.Query(`SELECT p.id,p.title,p.content,p.author_id,COALESCE((SELECT group_concat(c.name, ', ') FROM post_categories pc JOIN categories c ON c.id=pc.category_id WHERE pc.post_id=p.id),(SELECT c.name FROM categories c WHERE c.id=p.category_id),''),p.liked,p.disliked,COALESCE(p.image_path,'') FROM posts p WHERE p.author_id=? ORDER BY p.created_at DESC,p.id DESC LIMIT 20 OFFSET ?`, id, (page-1)*20)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var posts []Post
	for rows.Next() {
		var p Post
		if err = rows.Scan(&p.ID, &p.Title, &p.Content, &p.AuthorID, &p.Category, &p.LikeCount, &p.DislikeCount, &p.ImagePath); err != nil {
			return nil, err
		}
		posts = append(posts, p)
	}
	return posts, rows.Err()
}
func ProfileComments(db *sql.DB, id, page int) ([]ProfileComment, error) {
	rows, err := db.Query(`SELECT c.id,c.post_id,c.content,p.title,CAST(c.created_at AS TEXT) FROM comments c JOIN posts p ON p.id=c.post_id WHERE c.user_id=? ORDER BY c.created_at DESC,c.id DESC LIMIT 20 OFFSET ?`, id, (page-1)*20)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var comments []ProfileComment
	for rows.Next() {
		var c ProfileComment
		if err = rows.Scan(&c.ID, &c.PostID, &c.Content, &c.PostTitle, &c.CreatedAt); err != nil {
			return nil, err
		}
		comments = append(comments, c)
	}
	return comments, rows.Err()
}
func UpdateBio(db *sql.DB, id int, bio string) error {
	if len([]rune(bio)) > 500 {
		return fmt.Errorf("bio must be at most 500 characters")
	}
	_, err := db.Exec("UPDATE users SET bio=? WHERE id=?", bio, id)
	return err
}

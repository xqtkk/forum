package database

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
)

type Comment struct {
	ID           int
	PostID       int
	UserID       int
	Author       string
	Content      string
	CreatedAt    string
	LikeCount    int
	DislikeCount int
	Reaction     string
}

type Post struct {
	ID           int
	Title        string
	Content      string
	AuthorID     int
	Author       string
	Category     string
	CategoryIDs  []int
	LikeCount    int
	DislikeCount int
	ImagePath    string
	Reaction     string
	Comments     []Comment // Добавляем список комментариев
	CreatedAt    string
}

func (p Post) HasCategory(id int) bool {
	for _, categoryID := range p.CategoryIDs {
		if categoryID == id {
			return true
		}
	}
	return false
}

type Category struct {
	ID   int
	Name string
}

func GetPostByID(db *sql.DB, postID int, viewerID ...int) (Post, error) {
	var post Post
	userID := 0
	if len(viewerID) > 0 {
		userID = viewerID[0]
	}
	// Получаем пост
	query := `
		SELECT 
			p.id, 
			p.title, 
			p.content, 
			u.username AS author, 
			COALESCE((SELECT group_concat(c.name, ', ') FROM post_categories pc JOIN categories c ON c.id=pc.category_id WHERE pc.post_id=p.id), (SELECT c.name FROM categories c WHERE c.id=p.category_id), '') AS category,
			p.liked AS like_count, 
			p.disliked AS dislike_count, 
			COALESCE(p.image_path, '') AS image,
			p.author_id,
			COALESCE((SELECT reaction_type FROM post_reactions WHERE post_id=p.id AND user_id=?), '') AS reaction
		FROM posts p
		LEFT JOIN users u ON p.author_id = u.id 
		WHERE p.id = ?`

	// Выполняем запрос
	row := db.QueryRow(query, userID, postID)

	// Проверяем ошибку при сканировании
	err := row.Scan(&post.ID, &post.Title, &post.Content, &post.Author, &post.Category, &post.LikeCount, &post.DislikeCount, &post.ImagePath, &post.AuthorID, &post.Reaction)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return post, fmt.Errorf("post with ID %d not found", postID)
		}
		log.Printf("Error scanning row: %v", err)
		return post, err
	}
	categoryRows, err := db.Query(`SELECT category_id FROM post_categories WHERE post_id=? UNION SELECT category_id FROM posts WHERE id=? AND category_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM post_categories WHERE post_id=?)`, postID, postID, postID)
	if err != nil {
		return post, err
	}
	defer categoryRows.Close()
	for categoryRows.Next() {
		var categoryID int
		if err = categoryRows.Scan(&categoryID); err != nil {
			return post, err
		}
		post.CategoryIDs = append(post.CategoryIDs, categoryID)
	}
	if err = categoryRows.Err(); err != nil {
		return post, err
	}

	// Получаем комментарии для поста
	commentsQuery := `
		SELECT 
			c.id, 
			c.post_id, 
			c.user_id, 
			c.content, 
			c.created_at, 
			c.liked AS like_count, 
			c.disliked AS dislike_count, 
		 COALESCE((SELECT reaction_type FROM comment_reactions WHERE comment_id=c.id AND user_id=?), ''),
		 u.username AS author
		FROM comments c
		LEFT JOIN users u ON c.user_id = u.id
		WHERE c.post_id = ?`

	rows, err := db.Query(commentsQuery, userID, postID)
	if err != nil {
		log.Printf("error querying comments: %v", err)
		return post, err
	}
	defer rows.Close()

	// Заполняем список комментариев
	for rows.Next() {
		var comment Comment
		err := rows.Scan(&comment.ID, &comment.PostID, &comment.UserID, &comment.Content, &comment.CreatedAt, &comment.LikeCount, &comment.DislikeCount, &comment.Reaction, &comment.Author)
		if err != nil {
			log.Printf("skipping comment due to error: %v", err) // Логируем ошибку, но не прерываем выполнение
			continue
		}
		post.Comments = append(post.Comments, comment)
	}

	if err := rows.Err(); err != nil {
		log.Printf("Error iterating comments: %v", err)
		return post, err
	}

	return post, nil
}

func GetPosts(db *sql.DB, categoryID *int) ([]Post, error) { return getPosts(db, categoryID, 0, 0) }
func GetPostsPage(db *sql.DB, categoryID *int, page int) ([]Post, error) {
	return getPosts(db, categoryID, 20, (page-1)*20)
}
func getPosts(db *sql.DB, categoryID *int, limit, offset int) ([]Post, error) {
	query := `SELECT p.id,p.title,p.content,COALESCE((SELECT group_concat(c.name, ', ') FROM post_categories pc JOIN categories c ON c.id=pc.category_id WHERE pc.post_id=p.id),(SELECT c.name FROM categories c WHERE c.id=p.category_id),''),COALESCE(u.username,'Unknown'),p.liked,p.disliked,p.author_id,COALESCE(p.image_path,'') FROM posts p LEFT JOIN users u ON p.author_id=u.id`
	var args []interface{}
	if categoryID != nil {
		query += " WHERE EXISTS(SELECT 1 FROM post_categories pc WHERE pc.post_id=p.id AND pc.category_id=?) OR (NOT EXISTS(SELECT 1 FROM post_categories pc WHERE pc.post_id=p.id) AND p.category_id=?)"
		args = append(args, *categoryID, *categoryID)
	}
	query += " ORDER BY p.created_at DESC,p.id DESC"
	if limit > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, limit, offset)
	}
	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var posts []Post
	for rows.Next() {
		var post Post
		if err = rows.Scan(&post.ID, &post.Title, &post.Content, &post.Category, &post.Author, &post.LikeCount, &post.DislikeCount, &post.AuthorID, &post.ImagePath); err != nil {
			return nil, err
		}
		posts = append(posts, post)
	}
	return posts, rows.Err()
}

func GetPostsByUserID(db *sql.DB, userID int) ([]Post, error) {
	var posts []Post
	query := `
		SELECT p.id, p.title, p.content, COALESCE((SELECT group_concat(c.name, ', ') FROM post_categories pc JOIN categories c ON c.id=pc.category_id WHERE pc.post_id=p.id),(SELECT c.name FROM categories c WHERE c.id=p.category_id),'') AS category, p.liked AS like_count
        FROM posts p
        WHERE p.author_id = ?`

	rows, err := db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var post Post
		err := rows.Scan(&post.ID, &post.Title, &post.Content, &post.Category, &post.LikeCount)
		if err != nil {
			return nil, err
		}
		posts = append(posts, post)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return posts, nil
}

func GetLikedPostsByUserID(db *sql.DB, userID int) ([]Post, error) {
	var posts []Post

	query := `
		SELECT 
			p.id, p.title, p.content, COALESCE((SELECT group_concat(c.name, ', ') FROM post_categories pc JOIN categories c ON c.id=pc.category_id WHERE pc.post_id=p.id),(SELECT c.name FROM categories c WHERE c.id=p.category_id),'') AS category, 
			u.username AS author
		FROM posts p
		LEFT JOIN users u ON p.author_id = u.id
		INNER JOIN post_reactions pr ON p.id = pr.post_id
		WHERE pr.user_id = ? AND pr.reaction_type = 'like'
	`

	rows, err := db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var post Post
		err := rows.Scan(&post.ID, &post.Title, &post.Content, &post.Category, &post.Author)
		if err != nil {
			return nil, err
		}
		posts = append(posts, post)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}
	return posts, nil
}

func GetCategories(db *sql.DB) ([]Category, error) {
	rows, err := db.Query(`SELECT id, name FROM categories`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []Category
	for rows.Next() {
		var category Category
		err := rows.Scan(&category.ID, &category.Name)
		if err != nil {
			return nil, err
		}
		categories = append(categories, category)
	}

	return categories, nil
}

func GetPostIDByCommentID(db *sql.DB, commentID int) (postID int, err error) {
	query := `
		SELECT 
			p.post_id
		FROM comments p
		WHERE p.id = ?`

	// Выполняем запрос
	row := db.QueryRow(query, commentID)

	// Проверяем ошибку при сканировании
	err = row.Scan(&postID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return postID, errors.New("post not found")
		}
		log.Printf("Error scanning row: %v", err)
		return postID, err
	}

	return
}

func GetUserIDByCommentID(db *sql.DB, commentID int) (userID int, err error) {
	query := `
	SELECT 
		c.user_id
	FROM comments c
	WHERE c.id = ?`

	// Выполняем запрос
	row := db.QueryRow(query, commentID)

	// Проверяем ошибку при сканировании
	err = row.Scan(&userID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return userID, errors.New("post not found")
		}
		log.Printf("Error scanning row: %v", err)
		return userID, err
	}

	return
}

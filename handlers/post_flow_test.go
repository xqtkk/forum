package handlers

import (
	"bytes"
	"fmt"
	"forum/database"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func multipartRequest(t *testing.T, path string, values map[string]string, file []byte, cookie *http.Cookie) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range values {
		if err := writer.WriteField(key, value); err != nil {
			t.Fatal(err)
		}
	}
	if file != nil {
		part, err := writer.CreateFormFile("image", "original.png")
		if err != nil {
			t.Fatal(err)
		}
		part.Write(file)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "https://localhost:4000"+path, &body)
	r.Header.Set("Content-Type", writer.FormDataContentType())
	r.Header.Set("Origin", "https://localhost:4000")
	r.AddCookie(cookie)
	return r
}

func TestPostCreateUploadEditAndOwnerDelete(t *testing.T) {
	testDB(t)
	addUser(t, "writer@example.test", "writer", "user")
	cookie := loginCookie(t, "writer@example.test")
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(original)
	var picture bytes.Buffer
	if err = png.Encode(&picture, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	r := multipartRequest(t, "/create-post", map[string]string{"title": "First title", "content": "Post content"}, picture.Bytes(), cookie)
	w := httptest.NewRecorder()
	CreatePostHandler(w, r)
	if w.Code != 302 {
		t.Fatalf("create post %d %s", w.Code, w.Body.String())
	}
	db, _ := database.OpenDB()
	defer db.Close()
	var id int
	var path, createdAt string
	if err = db.QueryRow("SELECT id,image_path,created_at FROM posts").Scan(&id, &path, &createdAt); err != nil {
		t.Fatal(err)
	}
	if path == "uploads/original.png" || !strings.HasPrefix(path, "uploads/") {
		t.Fatal("image filename is not generated")
	}
	if _, err = os.Stat(filepath.FromSlash(path)); err != nil {
		t.Fatal(err)
	}
	r = multipartRequest(t, fmt.Sprintf("/edit-post?id=%d", id), map[string]string{"title": "Updated title", "content": "Updated content"}, nil, cookie)
	w = httptest.NewRecorder()
	EditPostHandler(w, r)
	if w.Code != 302 {
		t.Fatalf("edit post %d %s", w.Code, w.Body.String())
	}
	var newPath, newCreated, title string
	if err = db.QueryRow("SELECT title,image_path,created_at FROM posts WHERE id=?", id).Scan(&title, &newPath, &newCreated); err != nil {
		t.Fatal(err)
	}
	if title != "Updated title" || newPath != path || newCreated != createdAt {
		t.Fatal("edit lost attachment or changed publication date")
	}
	w = submit(PostHandler, fmt.Sprintf("/post?id=%d&action=delete", id), url.Values{}, cookie)
	if w.Code != 303 || w.Header().Get("Location") != "/" {
		t.Fatalf("delete post %d %s", w.Code, w.Header().Get("Location"))
	}
}

func TestImageUploadRejectsDisguisedFile(t *testing.T) {
	r := multipartRequest(t, "/create-post", map[string]string{}, []byte("<script>alert(1)</script>"), &http.Cookie{Name: "test", Value: "test"})
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		t.Fatal(err)
	}
	defer r.MultipartForm.RemoveAll()
	if _, err := savePostImage(r); err == nil {
		t.Fatal("accepted a fake PNG")
	}
}

func TestInvalidPostActionDoesNotRedirect(t *testing.T) {
	testDB(t)
	owner := addUser(t, "author@example.test", "author", "user")
	cookie := loginCookie(t, "author@example.test")
	db, _ := database.OpenDB()
	_, err := db.Exec("INSERT INTO posts(title,content,author_id) VALUES('Title','Content',?)", owner)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	w := submit(PostHandler, "/post?id=1&action=unknown", url.Values{}, cookie)
	if w.Code != 400 || w.Header().Get("Location") != "" {
		t.Fatalf("invalid action triggered redirect: %d", w.Code)
	}
}

func TestNotificationOwnershipAndRead(t *testing.T) {
	testDB(t)
	owner := addUser(t, "notice-owner@example.test", "notice_owner", "user")
	addUser(t, "notice-other@example.test", "notice_other", "user")
	db, _ := database.OpenDB()
	defer db.Close()
	if _, err := db.Exec("INSERT INTO notifications(user_id,notification_type) VALUES(?,'Test notification')", owner); err != nil {
		t.Fatal(err)
	}
	other := loginCookie(t, "notice-other@example.test")
	w := submit(NotificationsHandler, "/notifications?action=read&id=1", url.Values{}, other)
	if w.Code != 404 {
		t.Fatalf("other user changed notification: %d", w.Code)
	}
	ownerCookie := loginCookie(t, "notice-owner@example.test")
	w = submit(NotificationsHandler, "/notifications?action=read&id=1", url.Values{}, ownerCookie)
	if w.Code != 303 {
		t.Fatalf("owner cannot read notification: %d", w.Code)
	}
	var read bool
	if err := db.QueryRow("SELECT is_read FROM notifications WHERE id=1").Scan(&read); err != nil || !read {
		t.Fatal("notification did not become read")
	}
}

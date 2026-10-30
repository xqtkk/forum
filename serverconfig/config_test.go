package serverconfig

import (
	"crypto/tls"
	"forum/database"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLocalCertificateIsGeneratedPerInstallation(t *testing.T) {
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(original)
	t.Setenv("FORUM_ENV", "")
	t.Setenv("FORUM_HTTP", "")
	t.Setenv("PUBLIC_BASE_URL", "")
	t.Setenv("TLS_CERT_FILE", "")
	t.Setenv("TLS_KEY_FILE", "")
	t.Setenv("SMTP_HOST", "")
	t.Setenv("FORUM_DEV_MAIL", "")
	dir := t.TempDir()
	if err = os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	config, err := Configure("127.0.0.1:4000")
	if err != nil {
		t.Fatal(err)
	}
	if config == nil || config.MinVersion != tls.VersionTLS12 || len(config.Certificates) != 1 {
		t.Fatal("missing TLS configuration")
	}
	first := string(config.Certificates[0].Certificate[0])
	repeated, err := Configure("127.0.0.1:4000")
	if err != nil || string(repeated.Certificates[0].Certificate[0]) != first {
		t.Fatal("local certificate not reused")
	}
	another := t.TempDir()
	if err = os.Chdir(another); err != nil {
		t.Fatal(err)
	}
	other, err := Configure("127.0.0.1:4000")
	if err != nil || string(other.Certificates[0].Certificate[0]) == first {
		t.Fatal("different installations share a certificate")
	}
}

func TestPublicHttpNeedsExplicitTrustedProxy(t *testing.T) {
	t.Setenv("FORUM_HTTP", "1")
	t.Setenv("FORUM_ENV", "")
	t.Setenv("PUBLIC_BASE_URL", "http://example.test")
	t.Setenv("TRUSTED_PROXY_CIDRS", "")
	if _, err := Configure("0.0.0.0:4000"); err == nil {
		t.Fatal("public plain HTTP allowed without explicit proxy")
	}
	t.Setenv("FORUM_ENV", "production")
	t.Setenv("PUBLIC_BASE_URL", "https://example.test")
	t.Setenv("SMTP_HOST", "smtp.example.test")
	t.Setenv("TRUSTED_PROXY_CIDRS", "127.0.0.1/32")
	t.Setenv("REQUIRE_EMAIL_VERIFICATION", "")
	if _, err := Configure("0.0.0.0:4000"); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("REQUIRE_EMAIL_VERIFICATION") != "1" {
		t.Fatal("production does not require verified email")
	}
}

func TestBackupsAndRetentionOnlyTouchManagedFiles(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("FORUM_DB_PATH", filepath.Join(dir, "forum.db"))
	t.Setenv("FORUM_BACKUP_DIR", filepath.Join(dir, "backups"))
	db, err := database.InitDB()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	backupDir := os.Getenv("FORUM_BACKUP_DIR")
	if err = os.MkdirAll(backupDir, 0700); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(backupDir, "forum-"+time.Now().UTC().AddDate(0, 0, -20).Format("20060102")+".db")
	unrelated := filepath.Join(backupDir, "personal.db")
	os.WriteFile(old, []byte("old"), 0600)
	os.WriteFile(unrelated, []byte("keep"), 0600)
	if err = DailyBackup(db); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("old managed backup not removed")
	}
	if _, err = os.Stat(unrelated); err != nil {
		t.Fatal("unrelated file removed")
	}
	if err = DailyBackup(db); err != nil {
		t.Fatal("same-day backup is not idempotent")
	}
}

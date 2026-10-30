package handlers

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func deliverMail(to, subject, body string) error {
	recipient, err := mail.ParseAddress(to)
	if err != nil {
		return err
	}
	from := os.Getenv("SMTP_FROM")
	if from == "" {
		from = "Forum <no-reply@localhost>"
	}
	sender, err := mail.ParseAddress(from)
	if err != nil {
		return err
	}
	if strings.ContainsAny(to+from+subject, "\r\n") {
		return fmt.Errorf("invalid mail header")
	}
	message := "From: " + sender.String() + "\r\nTo: " + recipient.String() + "\r\nSubject: " + subject + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" + strings.ReplaceAll(body, "\n", "\r\n")
	host := os.Getenv("SMTP_HOST")
	if host == "" {
		if os.Getenv("FORUM_DEV_MAIL") != "1" {
			return fmt.Errorf("SMTP_HOST is not configured")
		}
		dir := os.Getenv("FORUM_MAIL_DIR")
		if dir == "" {
			dir = ".local/mail"
		}
		if err = os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, time.Now().UTC().Format("20060102T150405")+"-"+randomToken()[:10]+".eml"), []byte(message), 0600)
	}
	port := os.Getenv("SMTP_PORT")
	if port == "" {
		port = "587"
	}
	connection, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 5*time.Second)
	if err != nil {
		return err
	}
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(15 * time.Second))
	client, err := smtp.NewClient(connection, host)
	if err != nil {
		return err
	}
	defer client.Close()
	if ok, _ := client.Extension("STARTTLS"); ok {
		if err = client.StartTLS(&tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	} else if !(os.Getenv("SMTP_ALLOW_INSECURE_LOCAL") == "1" && (host == "localhost" || host == "127.0.0.1")) {
		return fmt.Errorf("SMTP server must support STARTTLS")
	}
	if user := os.Getenv("SMTP_USER"); user != "" {
		if err = client.Auth(smtp.PlainAuth("", user, os.Getenv("SMTP_PASSWORD"), host)); err != nil {
			return err
		}
	}
	if err = client.Mail(sender.Address); err != nil {
		return err
	}
	if err = client.Rcpt(recipient.Address); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err = writer.Write([]byte(message)); err != nil {
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	return client.Quit()
}

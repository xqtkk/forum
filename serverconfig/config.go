package serverconfig

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultSessionLifetime = 24 * time.Hour
	minSessionLifetime     = 5 * time.Minute
	maxSessionLifetime     = 30 * 24 * time.Hour
)

func SessionLifetime() (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv("SESSION_TTL"))
	if value == "" {
		return defaultSessionLifetime, nil
	}
	lifetime, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("SESSION_TTL must be a duration such as 24h: %w", err)
	}
	if lifetime < minSessionLifetime || lifetime > maxSessionLifetime {
		return 0, fmt.Errorf("SESSION_TTL must be between 5m and 720h")
	}
	return lifetime, nil
}

func Configure(addr string) (*tls.Config, error) {
	if _, err := SessionLifetime(); err != nil {
		return nil, err
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	loopback := host == "localhost" || (ip != nil && ip.IsLoopback())
	production := os.Getenv("FORUM_ENV") == "production"
	plain := os.Getenv("FORUM_HTTP") == "1"
	base := os.Getenv("PUBLIC_BASE_URL")
	if base == "" {
		scheme := "https"
		if plain {
			scheme = "http"
		}
		name := host
		if loopback {
			name = "localhost"
		}
		base = scheme + "://" + net.JoinHostPort(name, port)
		os.Setenv("PUBLIC_BASE_URL", base)
	}
	parsed, err := url.Parse(base)
	if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return nil, fmt.Errorf("PUBLIC_BASE_URL must be a complete origin with no path")
	}
	if production {
		if parsed.Scheme != "https" {
			return nil, fmt.Errorf("production requires an HTTPS PUBLIC_BASE_URL")
		}
		if os.Getenv("SMTP_HOST") == "" {
			return nil, fmt.Errorf("production requires SMTP_HOST for account verification and recovery")
		}
		os.Setenv("REQUIRE_EMAIL_VERIFICATION", "1")
	}
	for _, cidr := range strings.Split(os.Getenv("TRUSTED_PROXY_CIDRS"), ",") {
		if strings.TrimSpace(cidr) != "" {
			if _, _, err := net.ParseCIDR(strings.TrimSpace(cidr)); err != nil {
				return nil, fmt.Errorf("invalid trusted proxy CIDR: %w", err)
			}
		}
	}
	if plain {
		if !loopback && os.Getenv("TRUSTED_PROXY_CIDRS") == "" {
			return nil, fmt.Errorf("unencrypted HTTP may listen only on loopback, unless an explicit trusted TLS proxy is configured")
		}
		if production && os.Getenv("TRUSTED_PROXY_CIDRS") == "" {
			return nil, fmt.Errorf("production HTTP requires a trusted TLS proxy")
		}
		if loopback && !production && os.Getenv("SMTP_HOST") == "" && os.Getenv("FORUM_DEV_MAIL") == "" {
			os.Setenv("FORUM_DEV_MAIL", "1")
		}
		return nil, nil
	}
	certFile, keyFile := os.Getenv("TLS_CERT_FILE"), os.Getenv("TLS_KEY_FILE")
	if certFile == "" && keyFile == "" {
		if production || !loopback {
			return nil, fmt.Errorf("set TLS_CERT_FILE and TLS_KEY_FILE for public HTTPS")
		}
		certFile, keyFile, err = developmentCertificate()
		if err != nil {
			return nil, err
		}
		if os.Getenv("SMTP_HOST") == "" && os.Getenv("FORUM_DEV_MAIL") == "" {
			os.Setenv("FORUM_DEV_MAIL", "1")
		}
	} else if certFile == "" || keyFile == "" {
		return nil, fmt.Errorf("set both TLS_CERT_FILE and TLS_KEY_FILE")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, err
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{cert}}, nil
}
func developmentCertificate() (string, string, error) {
	dir := ".local/tls"
	certFile, keyFile := filepath.Join(dir, "localhost.crt"), filepath.Join(dir, "localhost.key")
	if cert, err := tls.LoadX509KeyPair(certFile, keyFile); err == nil {
		if parsed, err := x509.ParseCertificate(cert.Certificate[0]); err == nil && time.Until(parsed.NotAfter) > 24*time.Hour {
			return certFile, keyFile, nil
		}
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", "", err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return "", "", err
	}
	cert := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "Forum local development"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().AddDate(1, 0, 0), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", "", err
	}
	if err = os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), 0600); err != nil {
		return "", "", err
	}
	if err = os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0644); err != nil {
		return "", "", err
	}
	return certFile, keyFile, nil
}

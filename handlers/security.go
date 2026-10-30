package handlers

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Reject browser writes from other origins, including login CSRF.
// Proxy deployments must preserve the public Host and terminate TLS appropriately.
func ProtectRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			if !secureRequest(r) && os.Getenv("FORUM_ENV") == "production" {
				ErrorHandler(w, "HTTPS is required", 400)
				return
			}
			origin := r.Header.Get("Origin")
			if origin == "" {
				origin = r.Header.Get("Referer")
			}
			u, err := url.Parse(origin)
			scheme := "http"
			if secureRequest(r) {
				scheme = "https"
			}
			if err != nil || u.Host == "" || !strings.EqualFold(u.Host, r.Host) || u.Scheme != scheme || r.Header.Get("Sec-Fetch-Site") == "cross-site" {
				ErrorHandler(w, "This form must be submitted from this site. Reload the page and try again.", 403)
				return
			}
			bodyLimit := int64(12 << 20)
			if r.URL.Path == "/create-post" || r.URL.Path == "/edit-post" {
				bodyLimit = maxPostBodySize
			}
			r.Body = http.MaxBytesReader(w, r.Body, bodyLimit)
		}
		next.ServeHTTP(w, r)
	})
}

func trustedProxy(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	for _, cidr := range strings.Split(os.Getenv("TRUSTED_PROXY_CIDRS"), ",") {
		_, network, err := net.ParseCIDR(strings.TrimSpace(cidr))
		if err == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}
func secureRequest(r *http.Request) bool {
	return r.TLS != nil || (trustedProxy(r) && r.Header.Get("X-Forwarded-Proto") == "https")
}

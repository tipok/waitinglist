package handler

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP extracts the client's IP address from the request.
// It checks X-Forwarded-For (first entry), then X-Real-Ip, then
// falls back to r.RemoteAddr with the port stripped.
func ClientIP(r *http.Request) string {
	// 1. X-Forwarded-For: client, proxy1, proxy2
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if ip, _, _ := strings.Cut(xff, ","); ip != "" {
			return strings.TrimSpace(ip)
		}
	}

	// 2. X-Real-Ip
	if xri := r.Header.Get("X-Real-Ip"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// 3. RemoteAddr (host:port)
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr // already bare IP or unexpected format
	}
	return host
}

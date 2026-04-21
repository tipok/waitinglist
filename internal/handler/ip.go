package handler

import (
	"net"
	"net/http"
	"strings"
)

// ClientIP extracts the client's IP address from the request.
// It checks X-Forwarded-For (first entry), then X-Real-Ip, then
// falls back to r.RemoteAddr with port stripped.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for part := range strings.SplitSeq(xff, ",") {
			ip := strings.TrimSpace(part)
			if ip != "" {
				return ip
			}
		}
	}

	if xri := strings.TrimSpace(r.Header.Get("X-Real-Ip")); xri != "" {
		return xri
	}

	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

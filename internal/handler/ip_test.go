package handler

import (
	"net/http"
	"testing"
)

func TestClientIP(t *testing.T) {
	tests := []struct {
		name       string
		xff        string
		xRealIP    string
		remoteAddr string
		want       string
	}{
		{
			name: "single X-Forwarded-For",
			xff:  "203.0.113.50",
			want: "203.0.113.50",
		},
		{
			name: "multiple X-Forwarded-For",
			xff:  "203.0.113.50, 70.41.3.18, 150.172.238.178",
			want: "203.0.113.50",
		},
		{
			name: "X-Forwarded-For with spaces",
			xff:  "  203.0.113.50 , 70.41.3.18 ",
			want: "203.0.113.50",
		},
		{
			name:    "empty X-Forwarded-For with X-Real-Ip",
			xff:     "",
			xRealIP: "198.51.100.10",
			want:    "198.51.100.10",
		},
		{
			name:       "no proxy headers with port",
			remoteAddr: "192.0.2.1:12345",
			want:       "192.0.2.1",
		},
		{
			name:       "no proxy headers IPv6 with port",
			remoteAddr: "[::1]:12345",
			want:       "::1",
		},
		{
			name:       "no proxy headers bare IP",
			remoteAddr: "192.0.2.1",
			want:       "192.0.2.1",
		},
		{
			name:    "X-Forwarded-For takes precedence over X-Real-Ip",
			xff:     "203.0.113.50",
			xRealIP: "198.51.100.10",
			want:    "203.0.113.50",
		},
		{
			name:       "empty X-Forwarded-For entries fall through",
			xff:        " , , ",
			remoteAddr: "192.0.2.1:8080",
			want:       "192.0.2.1",
		},
		{
			name: "X-Forwarded-For with IPv6",
			xff:  "2001:db8::1",
			want: "2001:db8::1",
		},
		{
			name:       "empty RemoteAddr",
			remoteAddr: "",
			want:       "",
		},
		{
			name:       "empty X-Forwarded-For single space falls through",
			xff:        " ",
			remoteAddr: "192.0.2.1:9090",
			want:       "192.0.2.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &http.Request{
				Header:     http.Header{},
				RemoteAddr: tt.remoteAddr,
			}
			if tt.xff != "" {
				r.Header.Set("X-Forwarded-For", tt.xff)
			}
			if tt.xRealIP != "" {
				r.Header.Set("X-Real-Ip", tt.xRealIP)
			}

			got := ClientIP(r)
			if got != tt.want {
				t.Errorf("ClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

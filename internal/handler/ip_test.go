package handler

import (
	"net/http"
	"testing"
)

func TestClientIP_XForwardedForSingleIP(t *testing.T) {
	r := &http.Request{
		Header:     http.Header{"X-Forwarded-For": {"203.0.113.50"}},
		RemoteAddr: "192.0.2.1:12345",
	}
	if got := ClientIP(r); got != "203.0.113.50" {
		t.Errorf("expected 203.0.113.50, got %s", got)
	}
}

func TestClientIP_XForwardedForMultipleIPs(t *testing.T) {
	r := &http.Request{
		Header:     http.Header{"X-Forwarded-For": {"203.0.113.50, 70.41.3.18, 150.172.238.178"}},
		RemoteAddr: "192.0.2.1:12345",
	}
	if got := ClientIP(r); got != "203.0.113.50" {
		t.Errorf("expected 203.0.113.50, got %s", got)
	}
}

func TestClientIP_XForwardedForWithSpaces(t *testing.T) {
	r := &http.Request{
		Header:     http.Header{"X-Forwarded-For": {"  203.0.113.50 , 70.41.3.18"}},
		RemoteAddr: "192.0.2.1:12345",
	}
	if got := ClientIP(r); got != "203.0.113.50" {
		t.Errorf("expected 203.0.113.50, got %s", got)
	}
}

func TestClientIP_XRealIpFallback(t *testing.T) {
	r := &http.Request{
		Header:     http.Header{"X-Real-Ip": {"198.51.100.10"}},
		RemoteAddr: "192.0.2.1:12345",
	}
	if got := ClientIP(r); got != "198.51.100.10" {
		t.Errorf("expected 198.51.100.10, got %s", got)
	}
}

func TestClientIP_RemoteAddrFallback(t *testing.T) {
	r := &http.Request{
		Header:     http.Header{},
		RemoteAddr: "192.0.2.1:12345",
	}
	if got := ClientIP(r); got != "192.0.2.1" {
		t.Errorf("expected 192.0.2.1, got %s", got)
	}
}

func TestClientIP_RemoteAddrIPv6(t *testing.T) {
	r := &http.Request{
		Header:     http.Header{},
		RemoteAddr: "[::1]:8080",
	}
	if got := ClientIP(r); got != "::1" {
		t.Errorf("expected ::1, got %s", got)
	}
}

func TestClientIP_RemoteAddrWithoutPort(t *testing.T) {
	r := &http.Request{
		Header:     http.Header{},
		RemoteAddr: "192.0.2.1",
	}
	if got := ClientIP(r); got != "192.0.2.1" {
		t.Errorf("expected 192.0.2.1, got %s", got)
	}
}

func TestClientIP_XForwardedForPrecedence(t *testing.T) {
	r := &http.Request{
		Header: http.Header{
			"X-Forwarded-For": {"203.0.113.50"},
			"X-Real-Ip":       {"198.51.100.10"},
		},
		RemoteAddr: "192.0.2.1:12345",
	}
	if got := ClientIP(r); got != "203.0.113.50" {
		t.Errorf("expected 203.0.113.50, got %s", got)
	}
}

func TestClientIP_EmptyXForwardedForFallsThrough(t *testing.T) {
	r := &http.Request{
		Header:     http.Header{"X-Forwarded-For": {""}},
		RemoteAddr: "192.0.2.1:12345",
	}
	if got := ClientIP(r); got != "192.0.2.1" {
		t.Errorf("expected 192.0.2.1, got %s", got)
	}
}

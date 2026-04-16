package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestWriteJSON_StatusAndBody(t *testing.T) {
	w := httptest.NewRecorder()
	data := map[string]string{"key": "value"}

	WriteJSON(w, http.StatusOK, data, testLogger())

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %s", ct)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["key"] != "value" {
		t.Errorf("expected key=value, got key=%s", resp["key"])
	}
}

func TestWriteError_Format(t *testing.T) {
	w := httptest.NewRecorder()

	WriteError(w, http.StatusBadRequest, "something went wrong", testLogger())

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", w.Code)
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["error"] != "something went wrong" {
		t.Errorf("expected error message 'something went wrong', got '%s'", resp["error"])
	}
}

func TestWriteError_SpecialCharacters(t *testing.T) {
	w := httptest.NewRecorder()

	WriteError(w, http.StatusBadRequest, `message with "quotes" and newline\n`, testLogger())

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["error"] != `message with "quotes" and newline\n` {
		t.Errorf("unexpected error message: %s", resp["error"])
	}
}

func TestWriteError_SetsContentType(t *testing.T) {
	w := httptest.NewRecorder()

	WriteError(w, http.StatusInternalServerError, "error", testLogger())

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %s", ct)
	}
}

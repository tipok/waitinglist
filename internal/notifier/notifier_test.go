package notifier

import (
	"strings"
	"testing"

	"github.com/tipok/waitinglist/internal/config"
	"github.com/tipok/waitinglist/internal/model"
)

func TestNew_ReturnsNilWhenHostEmpty(t *testing.T) {
	n := New(config.SMTPConfig{}, nil)
	if n != nil {
		t.Fatal("expected nil notifier when SMTP host is empty")
	}
}

func TestNew_ReturnsNotifierWhenConfigured(t *testing.T) {
	n := New(config.SMTPConfig{Host: "localhost", Port: 1025}, nil)
	if n == nil {
		t.Fatal("expected non-nil notifier when SMTP host is configured")
	}
}

func TestRenderTemplate_ValidData(t *testing.T) {
	n := New(config.SMTPConfig{Host: "localhost", Port: 1025}, nil)

	data := templateData{
		Firstname:   "John",
		Lastname:    "Doe",
		ProjectName: "Beta App",
	}

	var buf strings.Builder
	//goland:noinspection ALL
	err := n.tmpl.Execute(&buf, data)
	if err != nil {
		t.Fatalf("unexpected template error: %v", err)
	}

	body := buf.String()
	if !strings.Contains(body, "John Doe") {
		t.Errorf("expected name in output, got: %s", body)
	}
	if !strings.Contains(body, "Beta App") {
		t.Errorf("expected project name in output, got: %s", body)
	}
}

func TestRenderTemplate_EmptyFields(t *testing.T) {
	n := New(config.SMTPConfig{Host: "localhost", Port: 1025}, nil)

	data := templateData{
		Firstname:   "",
		Lastname:    "",
		ProjectName: "",
	}

	var buf strings.Builder
	//goland:noinspection ALL
	err := n.tmpl.Execute(&buf, data)
	if err != nil {
		t.Fatalf("template should not error on empty fields: %v", err)
	}
}

func TestBuildMIMEMessage(t *testing.T) {
	msg := buildMIMEMessage("noreply@test.com", "user@test.com", "Welcome!", "<h1>Hi</h1>")
	s := string(msg)

	if !strings.Contains(s, "From: noreply@test.com\r\n") {
		t.Error("missing From header")
	}
	if !strings.Contains(s, "To: user@test.com\r\n") {
		t.Error("missing To header")
	}
	if !strings.Contains(s, "Subject: Welcome!\r\n") {
		t.Error("missing Subject header")
	}
	if !strings.Contains(s, "Content-Type: text/html; charset=UTF-8\r\n") {
		t.Error("missing Content-Type header")
	}
	if !strings.Contains(s, "<h1>Hi</h1>") {
		t.Error("missing body content")
	}
}

func TestNotifyAccessGranted_SkipsWhenNoFrom(t *testing.T) {
	n := New(config.SMTPConfig{Host: "localhost", Port: 1025}, nil)

	user := model.UserEntity{Firstname: "Jane", Lastname: "Doe", Email: "jane@test.com"}
	project := model.Project{Slug: "test", Name: "Test", Email: model.ProjectEmail{From: "", Subject: "Welcome"}}

	// Should not panic or attempt to send
	n.NotifyAccessGranted(user, project)
}

func TestNotifyAccessGranted_SkipsWhenNoSubject(t *testing.T) {
	n := New(config.SMTPConfig{Host: "localhost", Port: 1025}, nil)

	user := model.UserEntity{Firstname: "Jane", Lastname: "Doe", Email: "jane@test.com"}
	project := model.Project{Slug: "test", Name: "Test", Email: model.ProjectEmail{From: "noreply@test.com", Subject: ""}}

	// Should not panic or attempt to send
	n.NotifyAccessGranted(user, project)
}

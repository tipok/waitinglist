package notifier

import (
	"os"
	"path/filepath"
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

func TestLoadProjectTemplates_ValidFile(t *testing.T) {
	n := New(config.SMTPConfig{Host: "localhost", Port: 1025}, nil)

	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "custom.html")
	if err := os.WriteFile(tmplPath, []byte(`<p>Hello {{.Firstname}}</p>`), 0644); err != nil {
		t.Fatal(err)
	}

	err := n.LoadProjectTemplates(map[string]string{"beta": tmplPath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := n.projectTemplates["beta"]; !ok {
		t.Error("expected template for slug 'beta' to be loaded")
	}
}

func TestLoadProjectTemplates_InvalidPath(t *testing.T) {
	n := New(config.SMTPConfig{Host: "localhost", Port: 1025}, nil)

	err := n.LoadProjectTemplates(map[string]string{"beta": "/nonexistent/template.html"})
	if err == nil {
		t.Fatal("expected error for non-existent template path")
	}
}

func TestLoadProjectTemplates_InvalidTemplate(t *testing.T) {
	n := New(config.SMTPConfig{Host: "localhost", Port: 1025}, nil)

	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "broken.html")
	if err := os.WriteFile(tmplPath, []byte(`{{.Unclosed`), 0644); err != nil {
		t.Fatal(err)
	}

	err := n.LoadProjectTemplates(map[string]string{"beta": tmplPath})
	if err == nil {
		t.Fatal("expected error for invalid template syntax")
	}
}

func TestLoadProjectTemplates_EmptyPathSkipped(t *testing.T) {
	n := New(config.SMTPConfig{Host: "localhost", Port: 1025}, nil)

	err := n.LoadProjectTemplates(map[string]string{"beta": ""})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(n.projectTemplates) != 0 {
		t.Error("expected no templates loaded for empty path")
	}
}

func TestNotifyAccessGranted_UsesProjectTemplate(t *testing.T) {
	n := New(config.SMTPConfig{Host: "localhost", Port: 1025}, nil)

	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "custom.html")
	content := `<div>Custom: {{.Firstname}} {{.Lastname}} - {{.Email}} - {{.LoginURL}}</div>`
	if err := os.WriteFile(tmplPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := n.LoadProjectTemplates(map[string]string{"beta": tmplPath}); err != nil {
		t.Fatal(err)
	}

	tmpl := n.projectTemplates["beta"]
	data := templateData{
		Firstname:   "Alice",
		Lastname:    "Smith",
		ProjectName: "Beta App",
		Email:       "alice@test.com",
		LoginURL:    "https://beta.example.com",
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("unexpected template error: %v", err)
	}

	body := buf.String()
	if !strings.Contains(body, "Custom: Alice Smith") {
		t.Errorf("expected custom template output, got: %s", body)
	}
	if !strings.Contains(body, "alice@test.com") {
		t.Errorf("expected email in output, got: %s", body)
	}
	if !strings.Contains(body, "https://beta.example.com") {
		t.Errorf("expected login URL in output, got: %s", body)
	}
}

func TestNotifyAccessGranted_FallsBackToDefault(t *testing.T) {
	n := New(config.SMTPConfig{Host: "localhost", Port: 1025}, nil)

	data := templateData{
		Firstname:   "Bob",
		Lastname:    "Jones",
		ProjectName: "Default App",
		Email:       "bob@test.com",
		LoginURL:    "https://app.example.com",
	}

	var buf strings.Builder
	if err := n.tmpl.Execute(&buf, data); err != nil {
		t.Fatalf("unexpected template error: %v", err)
	}

	body := buf.String()
	if !strings.Contains(body, "Bob Jones") {
		t.Errorf("expected default template to render name, got: %s", body)
	}
	if !strings.Contains(body, "Default App") {
		t.Errorf("expected default template to render project name, got: %s", body)
	}
}

func TestRenderTemplate_IncludesEmailAndLoginURL(t *testing.T) {
	n := New(config.SMTPConfig{Host: "localhost", Port: 1025}, nil)

	dir := t.TempDir()
	tmplPath := filepath.Join(dir, "full.html")
	content := `{{.Firstname}} {{.Lastname}} {{.ProjectName}} {{.Email}} {{.LoginURL}}`
	if err := os.WriteFile(tmplPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	if err := n.LoadProjectTemplates(map[string]string{"proj": tmplPath}); err != nil {
		t.Fatal(err)
	}

	data := templateData{
		Firstname:   "Test",
		Lastname:    "User",
		ProjectName: "My Project",
		Email:       "test@example.com",
		LoginURL:    "https://myproject.com/login",
	}

	var buf strings.Builder
	if err := n.projectTemplates["proj"].Execute(&buf, data); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "Test User My Project test@example.com https://myproject.com/login"
	if buf.String() != expected {
		t.Errorf("expected %q, got %q", expected, buf.String())
	}
}

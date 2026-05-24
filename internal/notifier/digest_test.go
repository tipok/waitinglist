package notifier

import (
	"strings"
	"testing"

	"github.com/tipok/waitinglist/internal/config"
)

func TestRenderDigest_BothSections(t *testing.T) {
	data := DigestData{
		ProjectName: "Beta App",
		PeriodStart: "2025-05-20 00:00 UTC",
		PeriodEnd:   "2025-05-21 00:00 UTC",
		NewEnlisted: []EnlistedEntry{
			{Firstname: "Alice", Lastname: "Smith", Email: "alice@test.com", JoinedAt: "2025-05-20 10:00 UTC"},
			{Firstname: "Bob", Lastname: "Jones", Email: "bob@test.com", JoinedAt: "2025-05-20 14:00 UTC"},
		},
		NewGranted: []GrantedEntry{
			{Firstname: "Carol", Lastname: "White", Email: "carol@test.com", GrantedAt: "2025-05-20 12:00 UTC", GrantedBy: "admin"},
		},
		EnlistedCount: 2,
		GrantedCount:  1,
	}

	body, err := RenderDigest(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	checks := []string{
		"Beta App",
		"2025-05-20 00:00 UTC",
		"2025-05-21 00:00 UTC",
		"Alice Smith",
		"alice@test.com",
		"Bob Jones",
		"bob@test.com",
		"Carol White",
		"carol@test.com",
		"admin",
		"New Waiting List Entries (2)",
		"Access Granted (1)",
	}
	for _, want := range checks {
		if !strings.Contains(body, want) {
			t.Errorf("expected %q in output", want)
		}
	}

	if strings.Contains(body, "No activity during this period") {
		t.Error("should not contain 'no activity' message when there is activity")
	}
}

func TestRenderDigest_OnlyEnlisted(t *testing.T) {
	data := DigestData{
		ProjectName: "Test Project",
		PeriodStart: "2025-05-20 00:00 UTC",
		PeriodEnd:   "2025-05-21 00:00 UTC",
		NewEnlisted: []EnlistedEntry{
			{Firstname: "Dave", Lastname: "Brown", Email: "dave@test.com", JoinedAt: "2025-05-20 09:00 UTC"},
		},
		EnlistedCount: 1,
		GrantedCount:  0,
	}

	body, err := RenderDigest(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(body, "New Waiting List Entries (1)") {
		t.Error("expected enlisted section")
	}
	if strings.Contains(body, "Access Granted") {
		t.Error("should not contain granted section when no grants")
	}
}

func TestRenderDigest_OnlyGranted(t *testing.T) {
	data := DigestData{
		ProjectName: "Test Project",
		PeriodStart: "2025-05-20 00:00 UTC",
		PeriodEnd:   "2025-05-21 00:00 UTC",
		NewGranted: []GrantedEntry{
			{Firstname: "Eve", Lastname: "Green", Email: "eve@test.com", GrantedAt: "2025-05-20 11:00 UTC", GrantedBy: "scheduler"},
		},
		EnlistedCount: 0,
		GrantedCount:  1,
	}

	body, err := RenderDigest(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(body, "New Waiting List Entries") {
		t.Error("should not contain enlisted section when no enlisted")
	}
	if !strings.Contains(body, "Access Granted (1)") {
		t.Error("expected granted section")
	}
}

func TestSendDigest_SkipsEmptyRecipients(t *testing.T) {
	n := New(config.SMTPConfig{Host: "localhost", Port: 1025}, nil)

	data := DigestData{
		ProjectName:   "Test",
		NewEnlisted:   []EnlistedEntry{{Firstname: "A", Lastname: "B", Email: "a@b.c", JoinedAt: "now"}},
		EnlistedCount: 1,
	}

	err := n.SendDigest(nil, "from@test.com", "Subject", data)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}

	err = n.SendDigest([]string{}, "from@test.com", "Subject", data)
	if err != nil {
		t.Fatalf("expected nil error for empty slice, got: %v", err)
	}
}

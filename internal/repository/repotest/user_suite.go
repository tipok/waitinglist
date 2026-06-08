package repotest

import (
	"context"
	"testing"
	"time"

	"github.com/tipok/waitinglist/internal/model"
)

// RunUserRepositorySuite runs all shared user repository behavioral tests
// against the provided UserRepository. db is used for direct inserts
// needed by some tests (e.g. CountByAccess waiting_list row).
func RunUserRepositorySuite(t *testing.T, repo UserRepository, insertWaitingListRow func(t *testing.T, userID, projectSlug string)) {
	t.Helper()

	t.Run("Create", func(t *testing.T) {
		ctx := context.Background()
		user := NewUser("proj1", "create@repotest.com")
		if err := repo.Create(ctx, user); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if user.ID == "" {
			t.Error("expected ID to be set after Create")
		}
		if user.CreatedAt.IsZero() {
			t.Error("expected CreatedAt to be set after Create")
		}
		if user.HasAccess {
			t.Error("expected HasAccess=false after Create")
		}
	})

	t.Run("Create_DuplicateEmail", func(t *testing.T) {
		ctx := context.Background()
		u1 := NewUser("proj1", "dup@repotest.com")
		if err := repo.Create(ctx, u1); err != nil {
			t.Fatalf("first Create: %v", err)
		}
		u2 := NewUser("proj1", "dup@repotest.com")
		if err := repo.Create(ctx, u2); err != model.ErrDuplicateEmail {
			t.Errorf("expected ErrDuplicateEmail, got: %v", err)
		}
	})

	t.Run("GetByEmail_Found", func(t *testing.T) {
		ctx := context.Background()
		user := NewUser("proj1", "byemail@repotest.com")
		if err := repo.Create(ctx, user); err != nil {
			t.Fatalf("Create: %v", err)
		}
		got, err := repo.GetByEmail(ctx, "proj1", "byemail@repotest.com")
		if err != nil {
			t.Fatalf("GetByEmail: %v", err)
		}
		if got.ID != user.ID {
			t.Errorf("expected ID %s, got %s", user.ID, got.ID)
		}
		if got.Email != user.Email {
			t.Errorf("expected email %s, got %s", user.Email, got.Email)
		}
	})

	t.Run("GetByEmail_NotFound", func(t *testing.T) {
		ctx := context.Background()
		_, err := repo.GetByEmail(ctx, "proj1", "nobody@repotest.com")
		if err != model.ErrUserNotFound {
			t.Errorf("expected ErrUserNotFound, got: %v", err)
		}
	})

	t.Run("GetByID_Found", func(t *testing.T) {
		ctx := context.Background()
		user := NewUser("proj1", "byid@repotest.com")
		if err := repo.Create(ctx, user); err != nil {
			t.Fatalf("Create: %v", err)
		}
		got, err := repo.GetByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got.Email != user.Email {
			t.Errorf("expected email %s, got %s", user.Email, got.Email)
		}
	})

	t.Run("GetByID_NotFound", func(t *testing.T) {
		ctx := context.Background()
		_, err := repo.GetByID(ctx, "nonexistent-id")
		if err != model.ErrUserNotFound {
			t.Errorf("expected ErrUserNotFound, got: %v", err)
		}
	})

	t.Run("GetByIDs", func(t *testing.T) {
		ctx := context.Background()
		u1 := NewUser("proj1", "ids1@repotest.com")
		u2 := NewUser("proj1", "ids2@repotest.com")
		if err := repo.Create(ctx, u1); err != nil {
			t.Fatalf("Create u1: %v", err)
		}
		if err := repo.Create(ctx, u2); err != nil {
			t.Fatalf("Create u2: %v", err)
		}
		got, err := repo.GetByIDs(ctx, []string{u1.ID, u2.ID})
		if err != nil {
			t.Fatalf("GetByIDs: %v", err)
		}
		if len(got) != 2 {
			t.Errorf("expected 2 users, got %d", len(got))
		}
	})

	t.Run("GetByIDs_Empty", func(t *testing.T) {
		ctx := context.Background()
		got, err := repo.GetByIDs(ctx, nil)
		if err != nil {
			t.Fatalf("GetByIDs(nil): %v", err)
		}
		if got != nil {
			t.Errorf("expected nil, got %v", got)
		}
	})

	t.Run("GetUserInfoByEmails", func(t *testing.T) {
		ctx := context.Background()
		u := NewUser("proj1", "info@repotest.com")
		if err := repo.Create(ctx, u); err != nil {
			t.Fatalf("Create: %v", err)
		}
		infos, err := repo.GetUserInfoByEmails(ctx, "proj1", []string{"info@repotest.com", "noone@repotest.com"})
		if err != nil {
			t.Fatalf("GetUserInfoByEmails: %v", err)
		}
		if len(infos) != 1 {
			t.Errorf("expected 1 result, got %d", len(infos))
		}
	})

	t.Run("GetUserInfoByEmails_Empty", func(t *testing.T) {
		ctx := context.Background()
		infos, err := repo.GetUserInfoByEmails(ctx, "proj1", nil)
		if err != nil {
			t.Fatalf("GetUserInfoByEmails(nil): %v", err)
		}
		if len(infos) != 0 {
			t.Errorf("expected empty, got %v", infos)
		}
	})

	t.Run("GrantAccess", func(t *testing.T) {
		ctx := context.Background()
		user := NewUser("proj1", "grant@repotest.com")
		if err := repo.Create(ctx, user); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := repo.GrantAccess(ctx, []string{user.ID}, "admin"); err != nil {
			t.Fatalf("GrantAccess: %v", err)
		}
		got, err := repo.GetByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if !got.HasAccess {
			t.Error("expected HasAccess=true after GrantAccess")
		}
		if got.AccessGrantedBy == nil || *got.AccessGrantedBy != "admin" {
			t.Errorf("expected AccessGrantedBy='admin', got: %v", got.AccessGrantedBy)
		}
		if got.AccessGrantedAt == nil {
			t.Error("expected AccessGrantedAt to be set")
		}
	})

	t.Run("GrantAccess_InvalidSource", func(t *testing.T) {
		ctx := context.Background()
		err := repo.GrantAccess(ctx, []string{"any-id"}, "invalid-source")
		if err == nil {
			t.Error("expected error for invalid grant source")
		}
	})

	t.Run("GrantAccess_NotFound", func(t *testing.T) {
		ctx := context.Background()
		err := repo.GrantAccess(ctx, []string{"completely-nonexistent-id"}, "admin")
		if err != model.ErrUserNotFound {
			t.Errorf("expected ErrUserNotFound, got: %v", err)
		}
	})

	t.Run("GrantAccess_EmptyIDs", func(t *testing.T) {
		ctx := context.Background()
		if err := repo.GrantAccess(ctx, nil, "admin"); err != nil {
			t.Errorf("expected nil for empty ids, got: %v", err)
		}
	})

	t.Run("GrantAccess_ClearsPriorRevocation", func(t *testing.T) {
		ctx := context.Background()
		user := NewUser("proj1", "regrant@repotest.com")
		if err := repo.Create(ctx, user); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := repo.GrantAccess(ctx, []string{user.ID}, "admin"); err != nil {
			t.Fatalf("first grant: %v", err)
		}
		if err := repo.RevokeAccess(ctx, user.ID, "abuse", "admin1"); err != nil {
			t.Fatalf("revoke: %v", err)
		}
		if err := repo.GrantAccess(ctx, []string{user.ID}, "admin"); err != nil {
			t.Fatalf("re-grant: %v", err)
		}
		got, err := repo.GetByEmail(ctx, "proj1", "regrant@repotest.com")
		if err != nil {
			t.Fatalf("GetByEmail: %v", err)
		}
		if !got.HasAccess {
			t.Error("expected HasAccess=true after re-grant")
		}
		if got.AccessRevokedAt != nil || got.AccessRevokedBy != nil || got.AccessRevokeReason != nil {
			t.Error("expected revoke columns cleared after re-grant")
		}
	})

	t.Run("RevokeAccess", func(t *testing.T) {
		ctx := context.Background()
		user := NewUser("proj1", "revoke@repotest.com")
		if err := repo.Create(ctx, user); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := repo.GrantAccess(ctx, []string{user.ID}, "admin"); err != nil {
			t.Fatalf("GrantAccess: %v", err)
		}
		if err := repo.RevokeAccess(ctx, user.ID, "test reason", "admin-user"); err != nil {
			t.Fatalf("RevokeAccess: %v", err)
		}
		got, err := repo.GetByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("GetByID: %v", err)
		}
		if got.HasAccess {
			t.Error("expected HasAccess=false after RevokeAccess")
		}
		if got.AccessRevokeReason == nil || *got.AccessRevokeReason != "test reason" {
			t.Errorf("expected reason 'test reason', got: %v", got.AccessRevokeReason)
		}
		if got.AccessRevokedBy == nil || *got.AccessRevokedBy != "admin-user" {
			t.Errorf("expected AccessRevokedBy='admin-user', got: %v", got.AccessRevokedBy)
		}
		if got.AccessRevokedAt == nil {
			t.Error("expected AccessRevokedAt to be set")
		}
	})

	t.Run("RevokeAccess_EmptyReason", func(t *testing.T) {
		ctx := context.Background()
		err := repo.RevokeAccess(ctx, "any-id", "   ", "admin")
		if err != model.ErrRevokeReasonRequired {
			t.Errorf("expected ErrRevokeReasonRequired, got: %v", err)
		}
	})

	t.Run("RevokeAccess_NotFound", func(t *testing.T) {
		ctx := context.Background()
		err := repo.RevokeAccess(ctx, "nonexistent", "reason", "admin")
		if err != model.ErrUserNotFound {
			t.Errorf("expected ErrUserNotFound, got: %v", err)
		}
	})

	t.Run("CountByAccess", func(t *testing.T) {
		ctx := context.Background()
		u1 := NewUser("proj1", "cnt1@repotest.com")
		u2 := NewUser("proj1", "cnt2@repotest.com")
		if err := repo.Create(ctx, u1); err != nil {
			t.Fatalf("Create u1: %v", err)
		}
		if err := repo.Create(ctx, u2); err != nil {
			t.Fatalf("Create u2: %v", err)
		}
		if err := repo.GrantAccess(ctx, []string{u1.ID}, "admin"); err != nil {
			t.Fatalf("GrantAccess: %v", err)
		}
		insertWaitingListRow(t, u2.ID, "proj1")

		wl, access, err := repo.CountByAccess(ctx, "proj1")
		if err != nil {
			t.Fatalf("CountByAccess: %v", err)
		}
		if wl < 1 {
			t.Errorf("expected at least 1 waitlist entry, got %d", wl)
		}
		if access < 1 {
			t.Errorf("expected at least 1 access entry, got %d", access)
		}
	})

	t.Run("EnlistmentsByDay", func(t *testing.T) {
		ctx := context.Background()
		user := NewUser("proj1", "day@repotest.com")
		if err := repo.Create(ctx, user); err != nil {
			t.Fatalf("Create: %v", err)
		}
		counts, err := repo.EnlistmentsByDay(ctx, "proj1", 7)
		if err != nil {
			t.Fatalf("EnlistmentsByDay: %v", err)
		}
		if len(counts) != 7 {
			t.Errorf("expected 7 days, got %d", len(counts))
		}
		total := 0
		for _, dc := range counts {
			total += dc.Count
		}
		if total < 1 {
			t.Errorf("expected at least 1 total enlistment, got %d", total)
		}
	})

	t.Run("ListWithAccess", func(t *testing.T) {
		ctx := context.Background()
		u1 := NewUser("proj1", "lst1@repotest.com")
		u2 := NewUser("proj1", "lst2@repotest.com")
		if err := repo.Create(ctx, u1); err != nil {
			t.Fatalf("Create u1: %v", err)
		}
		if err := repo.Create(ctx, u2); err != nil {
			t.Fatalf("Create u2: %v", err)
		}
		if err := repo.GrantAccess(ctx, []string{u1.ID, u2.ID}, "admin"); err != nil {
			t.Fatalf("GrantAccess: %v", err)
		}
		users, err := repo.ListWithAccess(ctx, "proj1", "", 10, 0)
		if err != nil {
			t.Fatalf("ListWithAccess: %v", err)
		}
		if len(users) < 2 {
			t.Errorf("expected at least 2 users, got %d", len(users))
		}
	})

	t.Run("ListWithAccess_EmailFilter", func(t *testing.T) {
		ctx := context.Background()
		u1 := NewUser("proj1", "alice@repotest.com")
		u2 := NewUser("proj1", "bob@repotest.com")
		if err := repo.Create(ctx, u1); err != nil {
			t.Fatalf("Create u1: %v", err)
		}
		if err := repo.Create(ctx, u2); err != nil {
			t.Fatalf("Create u2: %v", err)
		}
		if err := repo.GrantAccess(ctx, []string{u1.ID, u2.ID}, "admin"); err != nil {
			t.Fatalf("GrantAccess: %v", err)
		}
		users, err := repo.ListWithAccess(ctx, "proj1", "alice", 10, 0)
		if err != nil {
			t.Fatalf("ListWithAccess with filter: %v", err)
		}
		if len(users) != 1 {
			t.Errorf("expected 1 user matching 'alice', got %d", len(users))
		}
		if users[0].Email != "alice@repotest.com" {
			t.Errorf("expected alice@repotest.com, got %s", users[0].Email)
		}
	})

	t.Run("ListAllWithAccess", func(t *testing.T) {
		ctx := context.Background()
		u := NewUser("proj1", "allaccess@repotest.com")
		if err := repo.Create(ctx, u); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := repo.GrantAccess(ctx, []string{u.ID}, "scheduler"); err != nil {
			t.Fatalf("GrantAccess: %v", err)
		}
		users, err := repo.ListAllWithAccess(ctx, "proj1")
		if err != nil {
			t.Fatalf("ListAllWithAccess: %v", err)
		}
		if len(users) < 1 {
			t.Errorf("expected at least 1 user, got %d", len(users))
		}
	})

	t.Run("GetGrantedSince", func(t *testing.T) {
		ctx := context.Background()
		before := time.Now().UTC().Add(-time.Second)

		u := NewUser("proj1", "since@repotest.com")
		if err := repo.Create(ctx, u); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := repo.GrantAccess(ctx, []string{u.ID}, "admin"); err != nil {
			t.Fatalf("GrantAccess: %v", err)
		}

		users, err := repo.GetGrantedSince(ctx, "proj1", before)
		if err != nil {
			t.Fatalf("GetGrantedSince: %v", err)
		}
		if len(users) < 1 {
			t.Errorf("expected at least 1 user granted since before, got %d", len(users))
		}

		future := time.Now().UTC().Add(time.Hour)
		users, err = repo.GetGrantedSince(ctx, "proj1", future)
		if err != nil {
			t.Fatalf("GetGrantedSince with future time: %v", err)
		}
		if len(users) != 0 {
			t.Errorf("expected 0 users with future since time, got %d", len(users))
		}
	})

	t.Run("GetUserInfoByEmails_IncludesRevokeReason", func(t *testing.T) {
		ctx := context.Background()
		user := NewUser("proj1", "revokeinfo@repotest.com")
		if err := repo.Create(ctx, user); err != nil {
			t.Fatalf("Create: %v", err)
		}
		if err := repo.GrantAccess(ctx, []string{user.ID}, "scheduler"); err != nil {
			t.Fatalf("GrantAccess: %v", err)
		}
		if err := repo.RevokeAccess(ctx, user.ID, "spam", "admin1"); err != nil {
			t.Fatalf("RevokeAccess: %v", err)
		}

		infos, err := repo.GetUserInfoByEmails(ctx, "proj1", []string{"revokeinfo@repotest.com"})
		if err != nil {
			t.Fatalf("GetUserInfoByEmails: %v", err)
		}
		if len(infos) != 1 {
			t.Fatalf("expected 1 row, got %d", len(infos))
		}
		got := infos[0]
		if got.HasAccess {
			t.Error("expected HasAccess=false")
		}
		if got.AccessRevokeReason == nil || *got.AccessRevokeReason != "spam" {
			t.Errorf("expected reason=spam, got %v", got.AccessRevokeReason)
		}
		if got.AccessRevokedAt == nil {
			t.Error("expected AccessRevokedAt populated")
		}
	})
}

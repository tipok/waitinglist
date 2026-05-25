package waitlist

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/tipok/waitinglist/internal/model"
	"github.com/tipok/waitinglist/internal/notifier"
)

type fakeDigestUserStore struct {
	users []model.UserEntity
	err   error
}

func (f *fakeDigestUserStore) GetGrantedSince(_ context.Context, _ string, _ time.Time) ([]model.UserEntity, error) {
	return f.users, f.err
}

type fakeDigestWaitlistStore struct {
	entries []model.WaitingListAdminRow
	err     error
}

func (f *fakeDigestWaitlistStore) GetEnlistedSince(_ context.Context, _ string, _ time.Time) ([]model.WaitingListAdminRow, error) {
	return f.entries, f.err
}

type fakeDigestSchedulerStore struct {
	lastSuccess   time.Time
	getErr        error
	updateCalled  bool
	updateProject string
	updateKey     string
}

func (f *fakeDigestSchedulerStore) GetLastSuccess(_ context.Context, _ string, _ string) (time.Time, error) {
	return f.lastSuccess, f.getErr
}

func (f *fakeDigestSchedulerStore) UpdateLastSuccess(_ context.Context, _ model.DBTX, project, key string) error {
	f.updateCalled = true
	f.updateProject = project
	f.updateKey = key
	return nil
}

type fakeDigestSender struct {
	called     bool
	recipients []string
	from       string
	subject    string
	data       notifier.DigestData
}

func (f *fakeDigestSender) SendDigest(recipients []string, from, subject string, data notifier.DigestData) error {
	f.called = true
	f.recipients = recipients
	f.from = from
	f.subject = subject
	f.data = data
	return nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestDigestScheduler_SkipsBeforeInterval(t *testing.T) {
	dur := model.Duration(24 * time.Hour)
	projects := []model.Project{
		{
			Slug: "test",
			Name: "Test",
			Digest: model.ProjectDigest{
				Recipients: []string{"admin@test.com"},
				Interval:   &dur,
				From:       "from@test.com",
			},
		},
	}

	scheduler := &fakeDigestSchedulerStore{
		lastSuccess: time.Now().Add(-1 * time.Hour), // only 1h ago, interval is 24h
	}
	sender := &fakeDigestSender{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately so goroutine exits

	StartDigest(ctx, testLogger(), projects, &fakeDigestUserStore{}, &fakeDigestWaitlistStore{}, scheduler, nil, sender)

	time.Sleep(50 * time.Millisecond)

	if sender.called {
		t.Error("expected digest to be skipped when interval hasn't elapsed")
	}
}

func TestDigestScheduler_SkipsNoActivity(t *testing.T) {
	dur := model.Duration(1 * time.Hour)
	projects := []model.Project{
		{
			Slug: "test",
			Name: "Test",
			Digest: model.ProjectDigest{
				Recipients: []string{"admin@test.com"},
				Interval:   &dur,
				From:       "from@test.com",
			},
		},
	}

	scheduler := &fakeDigestSchedulerStore{
		lastSuccess: time.Now().Add(-2 * time.Hour), // interval elapsed
	}
	userStore := &fakeDigestUserStore{users: nil}
	waitlistStore := &fakeDigestWaitlistStore{entries: nil}
	sender := &fakeDigestSender{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	StartDigest(ctx, testLogger(), projects, userStore, waitlistStore, scheduler, nil, sender)

	time.Sleep(50 * time.Millisecond)
	cancel()

	if sender.called {
		t.Error("expected no digest when there is no activity")
	}
}

func TestDigestScheduler_SendsOnActivity(t *testing.T) {
	dur := model.Duration(1 * time.Hour)
	grantedBy := "admin"
	grantedAt := time.Now().Add(-30 * time.Minute)
	projects := []model.Project{
		{
			Slug: "test",
			Name: "Test Project",
			Digest: model.ProjectDigest{
				Recipients: []string{"admin@test.com", "ops@test.com"},
				Interval:   &dur,
				From:       "digest@test.com",
				Subject:    "Test Digest",
			},
		},
	}

	scheduler := &fakeDigestSchedulerStore{
		lastSuccess: time.Now().Add(-2 * time.Hour),
	}
	userStore := &fakeDigestUserStore{
		users: []model.UserEntity{
			{Firstname: "Carol", Lastname: "W", Email: "carol@test.com", AccessGrantedAt: &grantedAt, AccessGrantedBy: &grantedBy},
		},
	}
	waitlistStore := &fakeDigestWaitlistStore{
		entries: []model.WaitingListAdminRow{
			{Firstname: "Alice", Lastname: "S", Email: "alice@test.com", CreatedAt: time.Now().Add(-1 * time.Hour)},
		},
	}
	sender := &fakeDigestSender{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	StartDigest(ctx, testLogger(), projects, userStore, waitlistStore, scheduler, nil, sender)

	time.Sleep(50 * time.Millisecond)
	cancel()

	if !sender.called {
		t.Fatal("expected digest to be sent when activity exists")
	}
	if len(sender.recipients) != 2 {
		t.Errorf("expected 2 recipients, got %d", len(sender.recipients))
	}
	if sender.from != "digest@test.com" {
		t.Errorf("expected from=digest@test.com, got %s", sender.from)
	}
	if sender.subject != "Test Digest" {
		t.Errorf("expected subject=Test Digest, got %s", sender.subject)
	}
	if sender.data.EnlistedCount != 1 {
		t.Errorf("expected 1 enlisted, got %d", sender.data.EnlistedCount)
	}
	if sender.data.GrantedCount != 1 {
		t.Errorf("expected 1 granted, got %d", sender.data.GrantedCount)
	}
}

func TestDigestScheduler_UpdatesState(t *testing.T) {
	dur := model.Duration(1 * time.Hour)
	projects := []model.Project{
		{
			Slug: "myproject",
			Name: "My Project",
			Digest: model.ProjectDigest{
				Recipients: []string{"admin@test.com"},
				Interval:   &dur,
				From:       "from@test.com",
			},
		},
	}

	scheduler := &fakeDigestSchedulerStore{
		lastSuccess: time.Now().Add(-2 * time.Hour),
	}
	waitlistStore := &fakeDigestWaitlistStore{
		entries: []model.WaitingListAdminRow{
			{Firstname: "User", Lastname: "One", Email: "u@test.com", CreatedAt: time.Now()},
		},
	}
	sender := &fakeDigestSender{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	StartDigest(ctx, testLogger(), projects, &fakeDigestUserStore{}, waitlistStore, scheduler, nil, sender)

	time.Sleep(50 * time.Millisecond)
	cancel()

	if !scheduler.updateCalled {
		t.Fatal("expected scheduler state to be updated")
	}
	if scheduler.updateProject != "myproject" {
		t.Errorf("expected project=myproject, got %s", scheduler.updateProject)
	}
	if scheduler.updateKey != schedulerKeyDigestLastSuccess {
		t.Errorf("expected key=%s, got %s", schedulerKeyDigestLastSuccess, scheduler.updateKey)
	}
}

func TestDigestScheduler_SkipsProjectWithNoRecipients(t *testing.T) {
	dur := model.Duration(1 * time.Hour)
	projects := []model.Project{
		{
			Slug: "no-digest",
			Name: "No Digest",
			Digest: model.ProjectDigest{
				Recipients: nil,
				Interval:   &dur,
			},
		},
		{
			Slug: "has-digest",
			Name: "Has Digest",
			Digest: model.ProjectDigest{
				Recipients: []string{"admin@test.com"},
				Interval:   &dur,
				From:       "from@test.com",
			},
		},
	}

	scheduler := &fakeDigestSchedulerStore{
		lastSuccess: time.Now().Add(-2 * time.Hour),
	}
	waitlistStore := &fakeDigestWaitlistStore{
		entries: []model.WaitingListAdminRow{
			{Firstname: "User", Lastname: "One", Email: "u@test.com", CreatedAt: time.Now()},
		},
	}
	sender := &fakeDigestSender{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	StartDigest(ctx, testLogger(), projects, &fakeDigestUserStore{}, waitlistStore, scheduler, nil, sender)

	time.Sleep(50 * time.Millisecond)
	cancel()

	if !sender.called {
		t.Fatal("expected digest to be sent for the project with recipients")
	}
	if sender.data.ProjectName != "Has Digest" {
		t.Errorf("expected ProjectName=Has Digest, got %s", sender.data.ProjectName)
	}
}

func TestDigestScheduler_FallsBackToEmailFrom(t *testing.T) {
	dur := model.Duration(1 * time.Hour)
	projects := []model.Project{
		{
			Slug:  "test",
			Name:  "Test",
			Email: model.ProjectEmail{From: "noreply@fallback.com"},
			Digest: model.ProjectDigest{
				Recipients: []string{"admin@test.com"},
				Interval:   &dur,
				From:       "",
			},
		},
	}

	scheduler := &fakeDigestSchedulerStore{
		lastSuccess: time.Now().Add(-2 * time.Hour),
	}
	waitlistStore := &fakeDigestWaitlistStore{
		entries: []model.WaitingListAdminRow{
			{Firstname: "U", Lastname: "X", Email: "u@x.com", CreatedAt: time.Now()},
		},
	}
	sender := &fakeDigestSender{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	StartDigest(ctx, testLogger(), projects, &fakeDigestUserStore{}, waitlistStore, scheduler, nil, sender)

	time.Sleep(50 * time.Millisecond)
	cancel()

	if sender.from != "noreply@fallback.com" {
		t.Errorf("expected fallback from=noreply@fallback.com, got %s", sender.from)
	}
}

func TestDigestScheduler_DefaultSubjectWhenEmpty(t *testing.T) {
	dur := model.Duration(1 * time.Hour)
	projects := []model.Project{
		{
			Slug: "test",
			Name: "Cool App",
			Digest: model.ProjectDigest{
				Recipients: []string{"admin@test.com"},
				Interval:   &dur,
				From:       "from@test.com",
				Subject:    "",
			},
		},
	}

	scheduler := &fakeDigestSchedulerStore{
		lastSuccess: time.Now().Add(-2 * time.Hour),
	}
	waitlistStore := &fakeDigestWaitlistStore{
		entries: []model.WaitingListAdminRow{
			{Firstname: "U", Lastname: "X", Email: "u@x.com", CreatedAt: time.Now()},
		},
	}
	sender := &fakeDigestSender{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	StartDigest(ctx, testLogger(), projects, &fakeDigestUserStore{}, waitlistStore, scheduler, nil, sender)

	time.Sleep(50 * time.Millisecond)
	cancel()

	expected := "Cool App — Activity Digest"
	if sender.subject != expected {
		t.Errorf("expected subject=%q, got %q", expected, sender.subject)
	}
}

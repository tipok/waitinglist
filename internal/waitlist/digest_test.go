package waitlist

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
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
	mu            sync.Mutex
	lastSuccess   time.Time
	getErr        error
	updateCalled  bool
	updateProject string
	updateKey     string
}

func (f *fakeDigestSchedulerStore) GetLastSuccess(_ context.Context, _ string, _ string) (time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastSuccess, f.getErr
}

func (f *fakeDigestSchedulerStore) UpdateLastSuccess(_ context.Context, _ model.DBTX, project, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updateCalled = true
	f.updateProject = project
	f.updateKey = key
	return nil
}

type fakeDigestSender struct {
	mu         sync.Mutex
	called     bool
	callCount  int
	recipients []string
	from       string
	subject    string
	data       notifier.DigestData
	err        error
}

func (f *fakeDigestSender) SendDigest(recipients []string, from, subject string, data notifier.DigestData) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.called = true
	f.callCount++
	f.recipients = recipients
	f.from = from
	f.subject = subject
	f.data = data
	return f.err
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestDigestScheduler_SkipsNoActivity(t *testing.T) {
	projects := []model.Project{
		{
			Slug: "test",
			Name: "Test",
			Digest: model.ProjectDigest{
				Recipients: []string{"admin@test.com"},
				Schedule:   "@every 1s",
				From:       "from@test.com",
			},
		},
	}

	scheduler := &fakeDigestSchedulerStore{
		lastSuccess: time.Now().Add(-2 * time.Hour),
	}
	userStore := &fakeDigestUserStore{users: nil}
	waitlistStore := &fakeDigestWaitlistStore{entries: nil}
	sender := &fakeDigestSender{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	StartDigest(ctx, testLogger(), projects, userStore, waitlistStore, scheduler, nil, sender)

	time.Sleep(1500 * time.Millisecond)
	cancel()

	if sender.called {
		t.Error("expected no digest when there is no activity")
	}
}

func TestDigestScheduler_SendsOnActivity(t *testing.T) {
	grantedBy := "admin"
	grantedAt := time.Now().Add(-30 * time.Minute)
	projects := []model.Project{
		{
			Slug: "test",
			Name: "Test Project",
			Digest: model.ProjectDigest{
				Recipients: []string{"admin@test.com", "ops@test.com"},
				Schedule:   "@every 1s",
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

	time.Sleep(1500 * time.Millisecond)
	cancel()

	sender.mu.Lock()
	defer sender.mu.Unlock()

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
	projects := []model.Project{
		{
			Slug: "myproject",
			Name: "My Project",
			Digest: model.ProjectDigest{
				Recipients: []string{"admin@test.com"},
				Schedule:   "@every 1s",
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

	time.Sleep(1500 * time.Millisecond)
	cancel()

	scheduler.mu.Lock()
	defer scheduler.mu.Unlock()

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
	projects := []model.Project{
		{
			Slug: "no-digest",
			Name: "No Digest",
			Digest: model.ProjectDigest{
				Recipients: nil,
				Schedule:   "@every 1s",
			},
		},
		{
			Slug: "has-digest",
			Name: "Has Digest",
			Digest: model.ProjectDigest{
				Recipients: []string{"admin@test.com"},
				Schedule:   "@every 1s",
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

	time.Sleep(1500 * time.Millisecond)
	cancel()

	sender.mu.Lock()
	defer sender.mu.Unlock()

	if !sender.called {
		t.Fatal("expected digest to be sent for the project with recipients")
	}
	if sender.data.ProjectName != "Has Digest" {
		t.Errorf("expected ProjectName=Has Digest, got %s", sender.data.ProjectName)
	}
}

func TestDigestScheduler_FallsBackToEmailFrom(t *testing.T) {
	projects := []model.Project{
		{
			Slug:  "test",
			Name:  "Test",
			Email: model.ProjectEmail{From: "noreply@fallback.com"},
			Digest: model.ProjectDigest{
				Recipients: []string{"admin@test.com"},
				Schedule:   "@every 1s",
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

	time.Sleep(1500 * time.Millisecond)
	cancel()

	sender.mu.Lock()
	defer sender.mu.Unlock()

	if sender.from != "noreply@fallback.com" {
		t.Errorf("expected fallback from=noreply@fallback.com, got %s", sender.from)
	}
}

func TestDigestScheduler_DefaultSubjectWhenEmpty(t *testing.T) {
	projects := []model.Project{
		{
			Slug: "test",
			Name: "Cool App",
			Digest: model.ProjectDigest{
				Recipients: []string{"admin@test.com"},
				Schedule:   "@every 1s",
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

	time.Sleep(1500 * time.Millisecond)
	cancel()

	sender.mu.Lock()
	defer sender.mu.Unlock()

	expected := "Cool App — Activity Digest"
	if sender.subject != expected {
		t.Errorf("expected subject=%q, got %q", expected, sender.subject)
	}
}

func TestDigestScheduler_GracefulShutdown(t *testing.T) {
	projects := []model.Project{
		{
			Slug: "test",
			Name: "Test",
			Digest: model.ProjectDigest{
				Recipients: []string{"admin@test.com"},
				Schedule:   "@every 1s",
				From:       "from@test.com",
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

	StartDigest(ctx, testLogger(), projects, &fakeDigestUserStore{}, waitlistStore, scheduler, nil, sender)

	time.Sleep(1500 * time.Millisecond)

	sender.mu.Lock()
	countBefore := sender.callCount
	sender.mu.Unlock()

	cancel()
	time.Sleep(200 * time.Millisecond)

	// Wait long enough that another tick would have fired
	time.Sleep(2 * time.Second)

	sender.mu.Lock()
	countAfter := sender.callCount
	sender.mu.Unlock()

	if countAfter != countBefore {
		t.Errorf("expected no more digest calls after shutdown, got %d additional calls", countAfter-countBefore)
	}
}

func TestDigestScheduler_ErrorDoesNotStopScheduler(t *testing.T) {
	var callCount atomic.Int32

	projects := []model.Project{
		{
			Slug: "test",
			Name: "Test",
			Digest: model.ProjectDigest{
				Recipients: []string{"admin@test.com"},
				Schedule:   "@every 1s",
				From:       "from@test.com",
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
	sender := &fakeDigestSender{err: errors.New("smtp connection refused")}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	countingSender := &countingDigestSender{inner: sender, count: &callCount}

	StartDigest(ctx, testLogger(), projects, &fakeDigestUserStore{}, waitlistStore, scheduler, nil, countingSender)

	time.Sleep(3500 * time.Millisecond)
	cancel()

	count := callCount.Load()
	if count < 2 {
		t.Errorf("expected at least 2 invocations despite errors, got %d", count)
	}
}

type countingDigestSender struct {
	inner *fakeDigestSender
	count *atomic.Int32
}

func (c *countingDigestSender) SendDigest(recipients []string, from, subject string, data notifier.DigestData) error {
	c.count.Add(1)
	return c.inner.SendDigest(recipients, from, subject, data)
}

func TestDigestScheduler_SkipsProjectWithNoSchedule(t *testing.T) {
	projects := []model.Project{
		{
			Slug: "no-schedule",
			Name: "No Schedule",
			Digest: model.ProjectDigest{
				Recipients: []string{"admin@test.com"},
				Schedule:   "",
				From:       "from@test.com",
			},
		},
	}

	sender := &fakeDigestSender{}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	StartDigest(ctx, testLogger(), projects, &fakeDigestUserStore{}, &fakeDigestWaitlistStore{}, &fakeDigestSchedulerStore{}, nil, sender)

	time.Sleep(50 * time.Millisecond)

	if sender.called {
		t.Error("expected no digest when schedule is empty")
	}
}

func TestProcessDigestProject_Direct(t *testing.T) {
	grantedBy := "admin"
	grantedAt := time.Now().Add(-30 * time.Minute)

	p := model.Project{
		Slug: "direct",
		Name: "Direct Test",
		Digest: model.ProjectDigest{
			Recipients: []string{"admin@test.com"},
			Schedule:   "0 9 * * *",
			From:       "digest@test.com",
			Subject:    "Direct Digest",
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

	processDigestProject(context.Background(), testLogger(), p, userStore, waitlistStore, scheduler, nil, sender)

	if !sender.called {
		t.Fatal("expected digest to be sent")
	}
	if sender.data.EnlistedCount != 1 {
		t.Errorf("expected 1 enlisted, got %d", sender.data.EnlistedCount)
	}
	if sender.data.GrantedCount != 1 {
		t.Errorf("expected 1 granted, got %d", sender.data.GrantedCount)
	}
}

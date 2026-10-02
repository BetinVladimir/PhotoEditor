package maintenance

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"testing"
)

type cleanupRepo struct {
	ticket   *OutputCleanup
	finished int
}

func (r *cleanupRepo) Claim(context.Context) (*OutputCleanup, error) { return r.ticket, nil }
func (r *cleanupRepo) Finish(context.Context, OutputCleanup) (bool, error) {
	r.finished++
	return true, nil
}

type orphanStore struct {
	calls   int
	failure bool
}

func (s *orphanStore) PurgeAttempt(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, int, int64) error {
	s.calls++
	if s.failure {
		return errors.New("storage unavailable")
	}
	return nil
}
func TestCleanupDoesNotReleaseFailedDeletionOrAcceptOtherKey(t *testing.T) {
	ticket := &OutputCleanup{UploadID: uuid.New(), Token: uuid.New(), TenantID: uuid.New(), TaskID: uuid.New(), ArtifactID: uuid.New(), Attempt: 1, Fence: 1, Bucket: "private"}
	ticket.Key, _ = objectstore.AttemptKey(ticket.TenantID, ticket.TaskID, ticket.ArtifactID, 1, 1)
	repo := &cleanupRepo{ticket: ticket}
	objects := &orphanStore{failure: true}
	cleaner := OutputCleaner{Repository: repo, Objects: objects, Bucket: "private"}
	if _, err := cleaner.CleanOne(context.Background()); err == nil || repo.finished != 0 {
		t.Fatal("failed removal released quota")
	}
	objects.failure = false
	original := ticket.Key
	ticket.Key = "tenants/another/key"
	if _, err := cleaner.CleanOne(context.Background()); err == nil || objects.calls != 1 {
		t.Fatal("arbitrary key reached delete")
	}
	ticket.Key = original
	ticket.Bucket = "other"
	if _, err := cleaner.CleanOne(context.Background()); err == nil || objects.calls != 1 {
		t.Fatal("wrong bucket reached delete")
	}
	ticket.Bucket = "private"
	if cleaned, err := cleaner.CleanOne(context.Background()); err != nil || !cleaned || repo.finished != 1 || objects.calls != 2 {
		t.Fatal("cleanup failed", err)
	}
	repo.ticket = nil
	if cleaned, err := cleaner.CleanOne(context.Background()); err != nil || cleaned {
		t.Fatal("empty cleanup is not idle")
	}
}

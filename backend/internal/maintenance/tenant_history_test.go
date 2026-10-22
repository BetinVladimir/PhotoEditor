package maintenance

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"testing"
)

type tenantHistoryProbe struct {
	objects *tenantObjects
	calls   int
	err     error
	cancel  context.CancelFunc
}

func (p *tenantHistoryProbe) RecordRequested(ctx context.Context, ticket TenantDeletion) error {
	p.calls++
	if p.cancel != nil {
		p.cancel()
	}
	if p.objects.calls != 0 {
		return errors.New("purge preceded durable history")
	}
	return p.err
}
func TestTenantCleanerRequiresDurableHistoryBeforeAnyPurge(t *testing.T) {
	for _, mode := range []string{"valid", "history_failure", "missing"} {
		t.Run(mode, func(t *testing.T) {
			repository := &tenantRepo{ticket: &TenantDeletion{JobID: uuid.New(), Token: uuid.New(), TenantID: uuid.New()}}
			objects := &tenantObjects{}
			history := &tenantHistoryProbe{objects: objects}
			cleaner := TenantCleaner{Repository: repository, Objects: objects, Batch: 1, RequireHistory: true, History: history}
			if mode == "history_failure" {
				history.err = errors.New("independent backup unavailable")
			}
			if mode == "missing" {
				cleaner.History = nil
			}
			changed, err := cleaner.CleanOne(context.Background())
			if (err == nil) != (mode == "valid") {
				t.Fatalf("mode=%s err=%v", mode, err)
			}
			if mode == "valid" {
				if !changed || history.calls != 1 || objects.calls != 2 || !repository.done {
					t.Fatal("history/purge/finish path incomplete")
				}
			} else if changed || objects.calls != 0 || repository.done || repository.checkpointed {
				t.Fatal("history refusal advanced deletion")
			}
		})
	}
}

func TestTenantCleanerRefusesHistoryThatReturnsAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repository := &tenantRepo{ticket: &TenantDeletion{JobID: uuid.New(), Token: uuid.New(), TenantID: uuid.New()}}
	objects := &tenantObjects{}
	history := &tenantHistoryProbe{objects: objects, cancel: cancel}
	changed, err := (TenantCleaner{Repository: repository, Objects: objects, Batch: 64, RequireHistory: true, History: history}).CleanOne(ctx)
	if changed || !errors.Is(err, context.Canceled) || history.calls != 1 || objects.calls != 0 || repository.done || repository.checkpointed {
		t.Fatal("late history acknowledgement began purge", changed, err, history, objects)
	}
}

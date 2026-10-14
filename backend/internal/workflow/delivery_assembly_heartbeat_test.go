package workflow

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

type assemblyHeartbeatStore struct {
	calls atomic.Int32
	fail  error
}

func (s *assemblyHeartbeatStore) Renew(_ context.Context, _ delivery.ExpansionLease) error {
	s.calls.Add(1)
	return s.fail
}
func (s *assemblyHeartbeatStore) ReadObservationPage(context.Context, delivery.ExpansionLease, delivery.PreparePlan, int, int) ([]delivery.ChildObservation, error) {
	return nil, nil
}

func assemblyHeartbeatLease() delivery.ExpansionLease {
	return delivery.ExpansionLease{Expansion: uuid.New(), Token: uuid.New(), Until: time.Now().Add(time.Minute),
		Tenant: uuid.New(), Run: uuid.New(), Workflow: uuid.New(), Package: uuid.New(), ParentTask: uuid.New(),
		SlotCount: 1, Cursor: 1}
}

func TestDeliveryAssemblyHeartbeatCoversPhysicalIOAndJoinsBeforeCommit(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	store := &assemblyHeartbeatStore{}
	result, err := withDeliveryAssemblyLease(ctx, store, assemblyHeartbeatLease(), time.Millisecond,
		func(work context.Context) (deliveryAssemblyIOResult, error) {
			for store.calls.Load() == 0 {
				select {
				case <-work.Done():
					return deliveryAssemblyIOResult{}, work.Err()
				case <-time.After(time.Millisecond):
				}
			}
			return deliveryAssemblyIOResult{state: "assemble", ref: v2input.Ref{ArtifactID: uuid.New()}}, nil
		})
	if err != nil || result.state != "assemble" || store.calls.Load() == 0 {
		t.Fatal("physical IO lost lease renewal", result, err)
	}
	before := store.calls.Load()
	time.Sleep(5 * time.Millisecond)
	if store.calls.Load() != before {
		t.Fatal("heartbeat continued into terminal commit")
	}
}

func TestDeliveryAssemblyHeartbeatLossCancelsStalePhysicalResult(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	lost := errors.New("delivery lease lost")
	store := &assemblyHeartbeatStore{fail: lost}
	result, err := withDeliveryAssemblyLease(ctx, store, assemblyHeartbeatLease(), time.Millisecond,
		func(work context.Context) (deliveryAssemblyIOResult, error) {
			<-work.Done()
			return deliveryAssemblyIOResult{state: "assemble"}, nil
		})
	if !errors.Is(err, lost) || result.state != "" || store.calls.Load() != 1 {
		t.Fatal("stale physical result reached terminal commit", result, err)
	}
}

package interchange

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type adoptionClaimStub struct {
	claim          *ExportAdoptionClaim
	renew          bool
	loseAfterFirst bool
	calls          int
	claimDeadline  time.Time
}

func (s *adoptionClaimStub) NextExportAdoption(ctx context.Context) (*ExportAdoptionClaim, error) {
	s.claimDeadline, _ = ctx.Deadline()
	return s.claim, nil
}
func (s *adoptionClaimStub) RenewExportAdoptionClaim(context.Context, ExportAdoptionClaim) (bool, error) {
	s.calls++
	if s.loseAfterFirst && s.calls > 1 {
		return false, nil
	}
	return s.renew, nil
}

type adoptionHandlerFunc func(context.Context, ExportAdoptionClaim) error

func (f adoptionHandlerFunc) PrepareAndAdopt(ctx context.Context, claim ExportAdoptionClaim) error {
	return f(ctx, claim)
}

func validProcessorClaim(t *testing.T) *ExportAdoptionClaim {
	t.Helper()
	tenant, actor := uuid.New(), uuid.New()
	key := "processor-claim"
	run := uuid.NewSHA1(tenant, []byte("interchange:create:"+actor.String()+":"+key))
	workflow := uuid.NewSHA1(run, []byte("workflow"))
	snapshot := uuid.New()
	request := CreateRequest{ProjectID: uuid.New(), Direction: "export", Format: "xmp", SnapshotID: &snapshot}
	sha, err := canonical.Hash(request)
	if err != nil {
		t.Fatal(err)
	}
	return &ExportAdoptionClaim{Tenant: tenant, Actor: actor, Run: run, Workflow: workflow,
		Task: uuid.NewSHA1(workflow, []byte("v2.interchange.export:0")), Result: uuid.New(),
		Token: uuid.New(), Key: key, RequestSHA: sha, Request: request}
}

func TestSchedulerExportAdoptionProcessorNoClaimAndSuccess(t *testing.T) {
	claims := &adoptionClaimStub{renew: true}
	handled := 0
	processor := SchedulerExportAdoptionProcessor{Claims: claims,
		Handler:           adoptionHandlerFunc(func(context.Context, ExportAdoptionClaim) error { handled++; return nil }),
		HeartbeatInterval: time.Minute}
	processed, err := processor.ProcessOne(context.Background())
	if err != nil || processed || handled != 0 {
		t.Fatal("empty queue invoked handler", processed, handled, err)
	}
	if claims.claimDeadline.IsZero() || time.Until(claims.claimDeadline) > 5*time.Second ||
		time.Until(claims.claimDeadline) <= 0 {
		t.Fatal("export claim discovery is not bounded")
	}
	claims.claim = validProcessorClaim(t)
	processed, err = processor.ProcessOne(context.Background())
	if err != nil || !processed || handled != 1 || claims.calls != 1 {
		t.Fatal("valid claim was not adopted", processed, handled, err)
	}
}

func TestSchedulerExportAdoptionProcessorRejectsStaleClaimBeforeObjectIO(t *testing.T) {
	claims := &adoptionClaimStub{claim: validProcessorClaim(t)}
	handled := false
	processor := SchedulerExportAdoptionProcessor{Claims: claims,
		Handler: adoptionHandlerFunc(func(context.Context, ExportAdoptionClaim) error {
			handled = true
			return nil
		}), HeartbeatInterval: time.Minute}
	processed, err := processor.ProcessOne(context.Background())
	if processed || !errors.Is(err, ErrExportAdoptionLeaseLost) || handled || claims.calls != 1 {
		t.Fatal("stale discovery reached object IO", processed, handled, claims.calls, err)
	}
}

func TestSchedulerExportAdoptionProcessorCancelsLostLease(t *testing.T) {
	claims := &adoptionClaimStub{claim: validProcessorClaim(t), renew: true, loseAfterFirst: true}
	processor := SchedulerExportAdoptionProcessor{Claims: claims,
		Handler: adoptionHandlerFunc(func(ctx context.Context, _ ExportAdoptionClaim) error {
			<-ctx.Done()
			return ctx.Err()
		}), HeartbeatInterval: time.Millisecond}
	processed, err := processor.ProcessOne(context.Background())
	if processed || !errors.Is(err, ErrExportAdoptionLeaseLost) || claims.calls != 2 {
		t.Fatal("lost lease did not stop handler", processed, claims.calls, err)
	}
}

func TestSchedulerExportAdoptionProcessorRejectsInvalidInstallation(t *testing.T) {
	processor := SchedulerExportAdoptionProcessor{}
	if processed, err := processor.ProcessOne(context.Background()); processed || !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("missing installation accepted", processed, err)
	}
	processor.HeartbeatInterval = 11 * time.Minute
	processor.Claims = &adoptionClaimStub{}
	processor.Handler = adoptionHandlerFunc(func(context.Context, ExportAdoptionClaim) error { return nil })
	if processed, err := processor.ProcessOne(context.Background()); processed || !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("unbounded heartbeat accepted", processed, err)
	}
	processor.HeartbeatInterval = time.Minute
	for _, budget := range []time.Duration{-time.Second, 2*time.Hour + time.Second} {
		processor.WorkTimeout = budget
		if processed, err := processor.ProcessOne(context.Background()); processed || !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("invalid export work budget accepted", budget, processed, err)
		}
	}
}

func TestSchedulerExportAdoptionProcessorBoundsPhysicalWork(t *testing.T) {
	claims := &adoptionClaimStub{claim: validProcessorClaim(t), renew: true}
	processor := SchedulerExportAdoptionProcessor{Claims: claims,
		Handler: adoptionHandlerFunc(func(ctx context.Context, _ ExportAdoptionClaim) error {
			<-ctx.Done()
			return nil // Even an incorrect handler cannot turn deadline into success.
		}), HeartbeatInterval: time.Minute, WorkTimeout: 10 * time.Millisecond}
	processed, err := processor.ProcessOne(context.Background())
	if processed || !errors.Is(err, context.DeadlineExceeded) || claims.calls != 1 {
		t.Fatal("export physical work exceeded deadline", processed, claims.calls, err)
	}
}

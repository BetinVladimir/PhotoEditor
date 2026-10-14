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
	claim *ExportAdoptionClaim
	renew bool
	calls int
}

func (s *adoptionClaimStub) NextExportAdoption(context.Context) (*ExportAdoptionClaim, error) {
	return s.claim, nil
}
func (s *adoptionClaimStub) RenewExportAdoptionClaim(context.Context, ExportAdoptionClaim) (bool, error) {
	s.calls++
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
	claims.claim = validProcessorClaim(t)
	processed, err = processor.ProcessOne(context.Background())
	if err != nil || !processed || handled != 1 {
		t.Fatal("valid claim was not adopted", processed, handled, err)
	}
}

func TestSchedulerExportAdoptionProcessorCancelsLostLease(t *testing.T) {
	claims := &adoptionClaimStub{claim: validProcessorClaim(t)}
	processor := SchedulerExportAdoptionProcessor{Claims: claims,
		Handler: adoptionHandlerFunc(func(ctx context.Context, _ ExportAdoptionClaim) error {
			<-ctx.Done()
			return ctx.Err()
		}), HeartbeatInterval: time.Millisecond}
	processed, err := processor.ProcessOne(context.Background())
	if processed || !errors.Is(err, ErrExportAdoptionLeaseLost) || claims.calls != 1 {
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
}

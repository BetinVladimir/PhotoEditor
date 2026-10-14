package v2draft

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type glareDraftBeginFake struct {
	calls    int
	identity GlareDonorDraftIdentity
}

func (f *glareDraftBeginFake) Begin(context.Context, GlareDonorDraftBeginRequest) (GlareDonorDraftIdentity, error) {
	f.calls++
	return f.identity, nil
}

func TestGlareSubmitterRecoversSealedRunBeforeDraftBegin(t *testing.T) {
	sha := strings.Repeat("a", 64)
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
	pin := v2portrait.RestorationQualificationPin{Operation: "portrait.glare",
		OperationVersion: "1", PolicyID: uuid.New(), QualificationID: uuid.New(),
		PolicySHA: sha, Report: ref(), Calibration: ref(), ModelLockSHA: sha,
		RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha,
		IssuerID: uuid.New(), Backend: "cpu"}
	proposal, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "model_lock_sha": pin.ModelLockSHA,
		"calibration": pin.Calibration})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := canonical.Marshal(map[string]any{
		"backend": "cpu", "renderer_version": "v2.2",
		"resource_estimate": v2portrait.LocalResourceEstimate{
			PixelCount: 100, RAMBytes: 3 * 1024 * 1024 * 1024,
			ScratchBytes: 1024, OutputBytes: 1024}})
	if err != nil {
		t.Fatal(err)
	}
	r := GlareDonorDraftBeginRequest{Tenant: uuid.New(), Project: uuid.New(),
		Shoot: uuid.New(), Snapshot: uuid.New(), Actor: auth.Identity{
			UserID: uuid.New(), SessionID: uuid.New()}, Key: "sealed-glare-1",
		Ordinal: 1, Proposal: proposal, Pin: pin, SourceObservation: uuid.New(),
		DonorObservation: uuid.New(), ReservedBytes: minRestorationDraftReservation}
	run, err := v2input.RunID(r.Tenant, r.Actor.UserID, r.Key)
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		t.Fatal(err)
	}
	task, err := v2input.TaskID(workflow, "portrait.glare", r.Ordinal)
	if err != nil {
		t.Fatal(err)
	}
	ticket := []byte(`{"run_id":"` + run.String() + `","state":"queued","task_id":"` +
		task.String() + `","workflow_id":"` + workflow.String() + `"}`)
	tx := &restorationStoreTx{role: "photo_scheduler", read: ticket}
	beginner := &glareDraftBeginFake{}
	s := GlareDonorRestorationSubmitter{Beginner: beginner,
		Coordinator: GlareDonorRestorationCoordinator{DB: restorationStoreDB{tx}}}
	got, err := s.Submit(context.Background(), r, profile)
	if err != nil || !got.Recovered || got.TaskID != task || beginner.calls != 0 ||
		tx.commits != 1 {
		t.Fatal("sealed replay attempted new draft or publication", err)
	}
	tx.read = nil
	beginner.identity = GlareDonorDraftIdentity{Run: uuid.New(), Workflow: workflow,
		BaseVersion: uuid.New(), DonorVersion: uuid.New()}
	if _, err := s.Submit(context.Background(), r, profile); err != v2input.ErrInvalid || beginner.calls != 1 {
		t.Fatal("mismatched begin identity reached graph admission", err)
	}
}

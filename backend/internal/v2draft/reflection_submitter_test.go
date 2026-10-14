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

type reflectionBeginFake struct {
	calls    int
	identity ReflectionDraftIdentity
}

func (f *reflectionBeginFake) BeginReflection(context.Context,
	ReflectionDraftBeginRequest) (ReflectionDraftIdentity, error) {
	f.calls++
	return f.identity, nil
}

func TestReflectionSubmitterRecoversExactSealedGraphBeforeDraft(t *testing.T) {
	sha := strings.Repeat("a", 64)
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
	pin := v2portrait.RestorationQualificationPin{Operation: "image.reflection",
		OperationVersion: "1", PolicyID: uuid.New(), QualificationID: uuid.New(),
		PolicySHA: sha, Report: ref(), Calibration: ref(), ModelLockSHA: sha,
		RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha,
		IssuerID: uuid.New(), Backend: "cpu"}
	base := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: sha}
	proposal, err := canonical.Marshal(map[string]any{"schema_version": 1,
		"base": base, "strength": .5, "mask": ref(), "protected": ref(),
		"model_lock_sha": pin.ModelLockSHA, "calibration": pin.Calibration})
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
	request := ReflectionDraftBeginRequest{Tenant: uuid.New(), Project: uuid.New(),
		Actor: auth.Identity{UserID: uuid.New(), SessionID: uuid.New()},
		Key:   "reflection-sealed-1", Pin: pin, ReservedBytes: minRestorationDraftReservation,
		Intent: ReflectionClientIntent{SchemaVersion: 1, Snapshot: uuid.New(),
			Ordinal: 2, Proposal: proposal, TargetPixel: ref(), ProtectedPixel: ref()}}
	run, err := v2input.RunID(request.Tenant, request.Actor.UserID, request.Key)
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		t.Fatal(err)
	}
	task, err := v2input.TaskID(workflow, "image.reflection", request.Intent.Ordinal)
	if err != nil {
		t.Fatal(err)
	}
	ticket, err := canonical.Marshal(RestorationAdmissionTicket{
		RunID: run, WorkflowID: workflow, TaskID: task, State: "queued"})
	if err != nil {
		t.Fatal(err)
	}
	tx := &restorationStoreTx{role: "photo_scheduler", read: ticket}
	beginner := &reflectionBeginFake{}
	submitter := ReflectionRestorationSubmitter{Beginner: beginner,
		Coordinator: ReflectionRestorationCoordinator{DB: restorationStoreDB{tx},
			CatalogProject: request.Project, BaseAssetVersion: base.AssetVersionID}}
	got, err := submitter.Submit(context.Background(), request, profile)
	if err != nil || !got.Recovered || got.Ticket.TaskID != task ||
		beginner.calls != 0 || tx.commits != 1 {
		t.Fatal("sealed reflection replay started another draft", err)
	}
	tx.read = nil
	beginner.identity = ReflectionDraftIdentity{Run: uuid.New(), Workflow: workflow,
		BaseVersion: base.AssetVersionID}
	if _, err := submitter.Submit(context.Background(), request, profile); err != v2input.ErrInvalid ||
		beginner.calls != 1 {
		t.Fatal("mismatched reflection draft identity reached graph admission", err)
	}
}

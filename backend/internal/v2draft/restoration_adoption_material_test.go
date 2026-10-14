package v2draft

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestRestorationCandidateMaterialBindsQueueIdentityBeforeReads(t *testing.T) {
	ref := func() v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(),
			SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	}
	params, err := canonical.Marshal(map[string]any{"schema_version": 1})
	if err != nil {
		t.Fatal(err)
	}
	job := RestorationAdoptionCandidate{Scope: RestorationArrayScope{
		Tenant: uuid.New(), Task: uuid.New(), Result: uuid.New()},
		Run: uuid.New(), Project: uuid.New(), Snapshot: uuid.New(),
		Actor:     auth.Identity{UserID: uuid.New(), SessionID: uuid.New()},
		Operation: "image.reflection", Params: params}
	plan := restorationTaskDescriptor{SchemaVersion: 1,
		Task:   v2input.DraftTask{ID: job.Scope.Task, Operation: job.Operation},
		Params: params, Origin: RestorationManualOrigin{Root: ref(), Params: ref(),
			TargetPixel: ref(), ProtectedPixel: ref()}}
	job.Descriptor, err = canonical.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	loader := RestorationCandidateMaterialLoader{Candidate: job,
		DB: &restorationBootstrapDB{}, BaseCatalog: &restorationCatalog{},
		Objects: restorationObjects{}}
	if _, err := loader.descriptor(job.Actor, job.Scope.Tenant, job.Operation, params); err != nil {
		t.Fatal("original queued descriptor refused", err)
	}
	wrong := job.Actor
	wrong.SessionID = uuid.New()
	if _, err := loader.Load(context.Background(), wrong, job.Scope.Tenant,
		job.Operation, params); err == nil {
		t.Fatal("session substitution reached material reads")
	}
	plan.Origin.TargetPixel = ref()
	changed, err := canonical.Marshal(plan)
	if err != nil {
		t.Fatal(err)
	}
	loader.Candidate.Descriptor = changed
	if _, err := loader.descriptor(job.Actor, job.Scope.Tenant,
		job.Operation, []byte("{}")); err == nil {
		t.Fatal("Params substitution accepted")
	}
}

package workflow

import (
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestRestorationPreviewSourceBindsOriginalIdentity(t *testing.T) {
	run := uuid.New()
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		t.Fatal(err)
	}
	ref := func() v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(),
			SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	}
	value := RestorationPreviewSource{Tenant: uuid.New(), Project: uuid.New(),
		Run: run, Workflow: workflow, Producer: uuid.New(), Review: uuid.New(),
		ResultID: uuid.New(), Result: ref(), Candidate: ref(), Attempt: 1, Fence: 1,
		Principal: auth.Identity{UserID: uuid.New(), SessionID: uuid.New()},
		Base: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(),
			CanvasHash: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}}
	if err := validateRestorationPreviewSource(value); err != nil {
		t.Fatal("original source refused", err)
	}
	changed := value
	changed.Workflow = uuid.New()
	if validateRestorationPreviewSource(changed) == nil {
		t.Fatal("workflow substitution accepted")
	}
	changed = value
	changed.Candidate = value.Base.Source
	if validateRestorationPreviewSource(changed) == nil {
		t.Fatal("candidate/base alias accepted")
	}
	changed = value
	changed.Principal.SessionID = uuid.Nil
	if validateRestorationPreviewSource(changed) == nil {
		t.Fatal("missing session accepted")
	}
}

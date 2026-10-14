package workflow

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func restorationPreviewFixture(t *testing.T) (RestorationPreviewPlan, RestorationPreviewSource, []byte, []byte) {
	t.Helper()
	fixture, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if err = json.Unmarshal(fixture, &images); err != nil {
		t.Fatal(err)
	}
	var candidate map[string]any
	if err = json.Unmarshal(images[0], &candidate); err != nil {
		t.Fatal(err)
	}
	baseRaw, err := canonical.Bytes(images[0])
	if err != nil {
		t.Fatal(err)
	}
	candidate["pixels"] = map[string]any{"artifact_id": uuid.New(), "sha256": strings.Repeat("b", 64)}
	candidateRaw, err := canonical.Marshal(candidate)
	if err != nil {
		t.Fatal(err)
	}
	run := uuid.New()
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		t.Fatal(err)
	}
	source := RestorationPreviewSource{Tenant: uuid.New(), Project: uuid.New(),
		Run: run, Workflow: workflow, Producer: uuid.New(), Review: uuid.New(),
		ResultID: uuid.New(), Result: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)},
		Candidate: v2input.Ref{ArtifactID: uuid.New(), SHA256: renderDerivativeBytesSHA(candidateRaw)},
		Attempt:   1, Fence: 1, Principal: auth.Identity{UserID: uuid.New(), SessionID: uuid.New()},
		Base: v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
			Source:     v2input.Ref{ArtifactID: uuid.New(), SHA256: renderDerivativeBytesSHA(baseRaw)},
			CanvasHash: strings.Repeat("d", 64)}}
	plan, err := CompileRestorationPreviewPlan(source, strings.Repeat("e", 64), baseRaw, candidateRaw)
	if err != nil {
		t.Fatal(err)
	}
	return plan, source, baseRaw, candidateRaw
}

func restorationPreviewFixturePlan(t *testing.T) RestorationPreviewPlan {
	plan, _, _, _ := restorationPreviewFixture(t)
	return plan
}

func TestRestorationPreviewPlanCompilesBothSidesAndRejectsSubstitution(t *testing.T) {
	plan, source, baseRaw, candidateRaw := restorationPreviewFixture(t)
	var err error
	docs := plan.Documents()
	if len(docs) != 4 {
		t.Fatal("missing before/after sizes")
	}
	for i, doc := range docs {
		wantSide := "before"
		wantRef := source.Base.Source
		if i >= 2 {
			wantSide, wantRef = "after", source.Candidate
		}
		if doc.Side != wantSide || doc.ID != restorationPreviewTaskID(plan.Run(), doc.Side, doc.Variant) ||
			len(doc.Inputs) < 2 || doc.Inputs[0].ID != wantRef.ArtifactID || doc.Inputs[0].SHA256 != wantRef.SHA256 {
			t.Fatal("wrong preview binding", i)
		}
		task, err := PrepareRestorationPreviewTask(doc, review.ArtifactRef{ID: uuid.New(), SHA256: doc.ParamsSHA})
		if err != nil || task.ID != doc.ID || task.NodeKey != restorationPreviewNodeKey(doc.Side, doc.Variant) {
			t.Fatal("invalid preview task", err)
		}
	}
	docs[0].Params[0] ^= 1
	docs[0].Inputs[0].ID = uuid.New()
	if plan.Documents()[0].Inputs[0].ID != source.Base.Source.ArtifactID ||
		plan.Documents()[0].ParamsSHA != renderDerivativeBytesSHA(plan.Documents()[0].Params) {
		t.Fatal("plan exposed mutable document")
	}
	if _, err = CompileRestorationPreviewPlan(source, strings.Repeat("e", 64), candidateRaw, candidateRaw); err == nil {
		t.Fatal("substituted base accepted")
	}
	bad := source
	bad.Candidate.SHA256 = source.Base.Source.SHA256
	if _, err = CompileRestorationPreviewPlan(bad, strings.Repeat("e", 64), baseRaw, candidateRaw); err == nil {
		t.Fatal("candidate digest substitution accepted")
	}
	var mismatched map[string]any
	if err = json.Unmarshal(candidateRaw, &mismatched); err != nil {
		t.Fatal(err)
	}
	mismatched["dtype"] = "uint16"
	mismatchRaw, err := canonical.Marshal(mismatched)
	if err != nil {
		t.Fatal(err)
	}
	bad = source
	bad.Candidate.SHA256 = renderDerivativeBytesSHA(mismatchRaw)
	if _, err = CompileRestorationPreviewPlan(bad, strings.Repeat("e", 64), baseRaw, mismatchRaw); err == nil {
		t.Fatal("candidate metadata change accepted")
	}
}

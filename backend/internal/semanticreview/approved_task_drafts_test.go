package semanticreview

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"testing"
)

func TestApprovedTaskDraftsPinOwnedCanonicalBytesAndChild(t *testing.T) {
	prepared, targets, request := decisionProofFixture(t)
	child := uuid.New()
	drafts, err := CompileApprovedTaskDrafts(child, prepared, targets, request)
	if err != nil || len(drafts) != 1 {
		t.Fatal(err)
	}
	draft := drafts[0]
	var edit ApprovedEdit
	if json.Unmarshal(draft.Body, &edit) != nil || edit.TargetID != draft.TargetID || edit.Kind != draft.Kind {
		t.Fatal("missing edit binding")
	}
	digest := sha256.Sum256(draft.Body)
	if draft.Params.SHA256 != hex.EncodeToString(digest[:]) || draft.TaskID == uuid.Nil || draft.Params.ArtifactID == uuid.Nil {
		t.Fatal("missing task pins")
	}
	again, err := CompileApprovedTaskDrafts(child, prepared, targets, request)
	if err != nil || again[0].TaskID != draft.TaskID || again[0].Params != draft.Params || !bytes.Equal(again[0].Body, draft.Body) {
		t.Fatal("unstable compilation", err)
	}
	drafts[0].Body[0] = '!'
	fresh, err := CompileApprovedTaskDrafts(child, prepared, targets, request)
	if err != nil || !bytes.Equal(fresh[0].Body, again[0].Body) {
		t.Fatal("caller mutated subsequent compilation")
	}
	other, err := CompileApprovedTaskDrafts(uuid.New(), prepared, targets, request)
	if err != nil || other[0].TaskID == draft.TaskID || other[0].Params.ArtifactID == draft.Params.ArtifactID {
		t.Fatal("cross-child identity reuse")
	}
	request.TargetDecisions[0].Decision = "reject"
	empty, err := CompileApprovedTaskDrafts(child, prepared, targets, request)
	if err != nil || len(empty) != 0 {
		t.Fatal("reject generated task")
	}
}

func TestApprovedTaskDraftsRejectInvalidChildAndStaleDecision(t *testing.T) {
	prepared, targets, request := decisionProofFixture(t)
	if result, err := CompileApprovedTaskDrafts(uuid.Nil, prepared, targets, request); err == nil || result != nil {
		t.Fatal("nil child accepted")
	}
	request.ExpectedRevision = "999"
	if result, err := CompileApprovedTaskDrafts(uuid.New(), prepared, targets, request); err == nil || result != nil {
		t.Fatal("stale decision compiled")
	}
}

package semanticreview

import (
	"github.com/google/uuid"
	"testing"
)

func TestApprovedEditPreservesPrivateIntentAndCandidatePins(t *testing.T) {
	prepared, targets, request := decisionProofFixture(t)
	edits, err := CompileApprovedEdits(prepared, targets, request)
	if err != nil || len(edits) != 1 {
		t.Fatal(err)
	}
	edit := edits[0]
	if edit.TargetID != request.TargetDecisions[0].TargetID || edit.Kind != "remove_object" || edit.Strength != .5 || !edit.PreservePersons || edit.Mask.ArtifactID == uuid.Nil || edit.Transform.ArtifactID == uuid.Nil || edit.Candidate.ArtifactID == uuid.Nil || edit.Source.Source.ArtifactID == uuid.Nil {
		t.Fatal("missing source-bound semantic edit")
	}
	prepared.IntentRef.ArtifactID = uuid.New()
	prepared.IntentRef.SHA256 = "invalid-public-projection"
	prepared.Intent.Kind = "local_exposure"
	ev := 1.0
	prepared.Intent.EV = &ev
	prepared.Intent.Strength = 0
	prepared.Intent.PreservePersons = false
	again, err := CompileApprovedEdits(prepared, targets, request)
	if err != nil || len(again) != 1 || again[0].Kind != edit.Kind || again[0].Strength != edit.Strength || again[0].EV != nil || !again[0].PreservePersons || again[0].Intent != edit.Intent || again[0].Mask != edit.Mask || again[0].Transform != edit.Transform {
		t.Fatal("mutable projection substituted edit", err)
	}
	request.TargetDecisions[0].Decision = "reject"
	none, err := CompileApprovedEdits(prepared, targets, request)
	if err != nil || len(none) != 0 {
		t.Fatal("rejection compiled an operation", err)
	}
}

func TestApprovedEditDeniesSubstitutedSourceInventory(t *testing.T) {
	prepared, targets, request := decisionProofFixture(t)
	targets[0].AssetVersionID = uuid.New()
	edits, err := CompileApprovedEdits(prepared, targets, request)
	if err == nil || len(edits) != 0 {
		t.Fatal("substituted inventory produced edits")
	}
}

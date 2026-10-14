package v2input

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestSemanticProposalConfigBindsIntentSnapshotQualificationAndGraph(t *testing.T) {
	ref := func() Ref { return Ref{uuid.New(), strings.Repeat("a", 64)} }
	source := SessionVersionBinding{uuid.New(), ref(), strings.Repeat("b", 64)}
	target := SessionVersionBinding{uuid.New(), ref(), strings.Repeat("c", 64)}
	run := uuid.New()
	workflow, _ := WorkflowID(run)
	snapshotID := uuid.New()
	intent := SemanticEditIntent{SchemaVersion: 1, Kind: "remove_object", Reference: source, ReferenceMask: ref(), TargetSnapshotID: snapshotID, Strength: .5, PreservePersons: true}
	intentBody, _ := canonical.Marshal(intent)
	intentRef := ref()
	intentRef.SHA256, _ = canonical.Hash(intent)
	snapshot, snapshotBody, err := CompilePinnedSessionInputSnapshot(snapshotID, []SessionSnapshotItem{{Ordinal: 0, Binding: target, WorkingImage: target.Source, Recipe: ref()}})
	if err != nil {
		t.Fatal(err)
	}
	snapshotRef := ref()
	snapshotRef.SHA256, _ = canonical.Hash(snapshot)
	qualification := SemanticQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), PolicySHA: strings.Repeat("b", 64), Report: ref(), Calibration: ref(), ModelLockSHA: strings.Repeat("c", 64), RuntimeLockSHA: strings.Repeat("d", 64), DeviceProfileSHA: strings.Repeat("e", 64), DatasetSHA: strings.Repeat("f", 64), IssuerID: uuid.New(), Backend: "cuda"}
	params := SemanticTransferParams{1, intentRef, source, target, qualification.ModelLockSHA, qualification.Calibration}
	paramsRef := ref()
	paramsRef.SHA256, _ = canonical.Hash(params)
	dependencies := SemanticTransferDependencies{ReferenceWidth: 24, ReferenceHeight: 20, TargetWidth: 24, TargetHeight: 20, ReferenceGamma: true, TargetGamma: true, Reference: CaptureImageInputs{Pixels: ref()}, Target: CaptureImageInputs{Pixels: ref()}, SelectedPixels: ref(), SourceProtection: ref(), TargetProtection: ref(), SourceProtectionPixels: ref(), TargetProtectionPixels: ref()}
	profile, err := CompileSemanticTransferProfile(24, 20, 24, 20, true, true, SemanticQualifiedCosts{}, 1<<40, 1<<40, 1<<40, 1<<40)
	if err != nil {
		t.Fatal(err)
	}
	task, err := CompileSemanticTransferTask(workflow, 0, params, intent, paramsRef, dependencies, profile)
	if err != nil {
		t.Fatal(err)
	}
	project, session := uuid.New(), uuid.New()
	config, body, digest, err := CompileSemanticProposalConfig(project, session, run, intentRef, intentBody, snapshotRef, snapshotBody, []FrozenSemanticTask{task}, qualification)
	if err != nil || len(body) == 0 || len(digest) != 64 || config.Semantic.Qualification == nil || config.Semantic.SnapshotDigest != snapshot.SnapshotDigest {
		t.Fatal("proposal config", err)
	}
	for _, kind := range []string{"intent", "run", "qualification", "snapshot"} {
		badIntent, badRun, badQualification, badSnapshot := intentRef, run, qualification, snapshotRef
		switch kind {
		case "intent":
			badIntent.ArtifactID = uuid.New()
		case "run":
			badRun = uuid.New()
		case "qualification":
			badQualification.ModelLockSHA = strings.Repeat("0", 64)
		case "snapshot":
			badSnapshot.SHA256 = strings.Repeat("0", 64)
		}
		if _, _, _, err := CompileSemanticProposalConfig(project, session, badRun, badIntent, intentBody, badSnapshot, snapshotBody, []FrozenSemanticTask{task}, badQualification); err == nil {
			t.Fatal("mixed proposal accepted", kind)
		}
	}
}

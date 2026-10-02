package v2input

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestDecodeSessionAnalysisRecoveryUsesExactFrozenInputs(t *testing.T) {
	run := uuid.New()
	image := Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	snapshot, _, err := CompileSessionInputSnapshot(run, []SessionSnapshotItem{{
		Ordinal: 0, Binding: SessionVersionBinding{AssetVersionID: uuid.New(),
			Source: image, CanvasHash: strings.Repeat("b", 64)},
		WorkingImage: image,
		Recipe:       Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	context, err := canonical.Marshal(sessionPlanContext{SchemaVersion: 1,
		SnapshotID: snapshot.SnapshotID, SnapshotDigest: snapshot.SnapshotDigest,
		ManualReferences: []sessionManualReference{}, ManualLockedOrdinals: []int{}})
	if err != nil {
		t.Fatal(err)
	}
	contextSHA, err := canonical.Hash(json.RawMessage(context))
	if err != nil {
		t.Fatal(err)
	}
	workflow, _ := WorkflowID(run)
	probe := SessionAnalysisReplayResult{Outcome: "pending", Run: run,
		Workflow: workflow, RequestSHA: strings.Repeat("d", 64),
		PolicySHA: strings.Repeat("e", 64)}
	recovery := sessionAnalysisRecovery{SchemaVersion: 1, Snapshot: snapshot,
		Context: context, ContextSHA: contextSHA, PolicySHA: probe.PolicySHA,
		ModelLockSHA: strings.Repeat("f", 64), ReservedBytes: 1024}
	raw, err := canonical.Marshal(recovery)
	if err != nil {
		t.Fatal(err)
	}
	result, err := decodeSessionAnalysisRecovery(raw, run, probe)
	if err != nil || result.Outcome != "pending" ||
		result.ModelLockSHA != recovery.ModelLockSHA ||
		result.Snapshot.SnapshotDigest != snapshot.SnapshotDigest {
		t.Fatalf("pending recovery: %+v %v", result, err)
	}
	recovery.Snapshot.Items = append([]SessionSnapshotItem(nil), snapshot.Items...)
	recovery.Snapshot.Items[0].Recipe.SHA256 = strings.Repeat("0", 64)
	broken, _ := canonical.Marshal(recovery)
	if _, err := decodeSessionAnalysisRecovery(broken, run, probe); err == nil {
		t.Fatal("accepted changed recipe without matching frozen digest")
	}
	recovery.Snapshot = snapshot
	recovery.ContextSHA = strings.Repeat("0", 64)
	broken, _ = canonical.Marshal(recovery)
	if _, err := decodeSessionAnalysisRecovery(broken, run, probe); err == nil {
		t.Fatal("accepted changed context hash")
	}
}

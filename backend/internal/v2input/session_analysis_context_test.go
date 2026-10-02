package v2input

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestCompileSessionPlanContextUsesFrozenManualReferences(t *testing.T) {
	run := uuid.New()
	items := make([]SessionSnapshotItem, 2)
	for ordinal := range items {
		image := Ref{uuid.New(), strings.Repeat("a", 64)}
		items[ordinal] = SessionSnapshotItem{Ordinal: ordinal,
			Binding: SessionVersionBinding{AssetVersionID: uuid.New(),
				Source: image, CanvasHash: strings.Repeat("b", 64)},
			WorkingImage: image, Recipe: Ref{uuid.New(), strings.Repeat("c", 64)}}
	}
	snapshot, _, err := CompileSessionInputSnapshot(run, items)
	if err != nil {
		t.Fatal(err)
	}
	manual := []sessionManualReference{{AssetVersionID: items[1].Binding.AssetVersionID,
		PreviousMembers: []uuid.UUID{items[0].Binding.AssetVersionID,
			items[1].Binding.AssetVersionID}}}
	project := uuid.New()
	frozen, err := canonical.Marshal(sessionAnalysisPreflight{SessionID: uuid.New(),
		ProjectID: project, SessionRevision: "2", CollectionID: uuid.New(),
		SelectionSnapshotID: uuid.New(), SelectionDigest: strings.Repeat("d", 64),
		ItemCount: "2", ManualReferences: manual,
		FrozenSnapshotID:     snapshot.SnapshotID,
		FrozenSnapshotDigest: snapshot.SnapshotDigest})
	if err != nil {
		t.Fatal(err)
	}
	body, sha, gotProject, err := CompileSessionPlanContext(frozen, snapshot)
	if err != nil || gotProject != project || !digestPattern.MatchString(sha) {
		t.Fatalf("compile: %v", err)
	}
	var context sessionPlanContext
	if json.Unmarshal(body, &context) != nil || len(context.ManualLockedOrdinals) != 1 ||
		context.ManualLockedOrdinals[0] != 1 {
		t.Fatal("manual lock does not match selected ordinal")
	}
	broken := append([]byte(nil), frozen...)
	var fields map[string]any
	if json.Unmarshal(broken, &fields) != nil {
		t.Fatal("invalid test input")
	}
	fields["frozen_snapshot_digest"] = strings.Repeat("e", 64)
	broken, _ = canonical.Marshal(fields)
	if _, _, _, err := CompileSessionPlanContext(broken, snapshot); err == nil {
		t.Fatal("accepted different frozen digest")
	}
}

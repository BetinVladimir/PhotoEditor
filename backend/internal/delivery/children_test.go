package delivery

import (
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestDeliveryChildrenPageIdentityAndOwnership(t *testing.T) {
	snapshot, raw, policy, configs := freezeFixture(t)
	_, frozenRaw, sha, err := CompileFrozenInput(snapshot, raw, policy, configs)
	if err != nil {
		t.Fatal(err)
	}
	frozen := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	project, session, parent := uuid.New(), uuid.New(), uuid.New()
	workflow, _ := v2input.WorkflowID(parent)
	_, _, params, err := CompilePrepareProof(project, session, frozen, frozenRaw, raw, configs)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := CompilePreparePlan(workflow, project, session, v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(params)}, frozen, frozenRaw, raw, configs)
	if err != nil {
		t.Fatal(err)
	}
	page, err := plan.ExportChildren(parent, 0, 100)
	if err != nil || !page.Done || page.NextCursor != 1 || len(page.Items) != 1 {
		t.Fatal("page", err)
	}
	expected := uuid.NewSHA1(parent, []byte("child:export:0"))
	if page.Items[0].RunID != expected || page.Items[0].Preset.Config != configs[0].Ref {
		t.Fatal("identity/config drift")
	}
	page.Items[0].Preset.PresetID = uuid.New()
	again, err := plan.ExportChildren(parent, 0, 1)
	if err != nil || again.Items[0].Preset.PresetID != configs[0].ID {
		t.Fatal("borrowed item")
	}
	terminal, err := plan.ExportChildren(parent, 1, 100)
	if err != nil || !terminal.Done || len(terminal.Items) != 0 {
		t.Fatal("terminal page")
	}
	for _, args := range [][2]int{{-1, 1}, {2, 1}, {0, 0}, {0, 101}} {
		if _, err := plan.ExportChildren(parent, args[0], args[1]); err == nil {
			t.Fatal("invalid cursor/limit", fmt.Sprint(args))
		}
	}
	if _, err := plan.ExportChildren(uuid.New(), 0, 1); err == nil {
		t.Fatal("foreign parent accepted")
	}
}

func TestDeliveryChildrenCursorCrossesPresetAndPageBoundaries(t *testing.T) {
	parent := uuid.New()
	workflow, _ := v2input.WorkflowID(parent)
	items := make([]v2input.SessionSnapshotItem, 60)
	for i := range items {
		items[i].Ordinal = i
		items[i].Binding.AssetVersionID = uuid.New()
	}
	presets := []FrozenPreset{{PresetID: uuid.New()}, {PresetID: uuid.New()}}
	plan := PreparePlan{workflow: workflow, task: uuid.New(), items: items, presets: presets, preparation: Preparation{SlotCount: 120}}
	first, err := plan.ExportChildren(parent, 0, 100)
	if err != nil || first.Done || first.NextCursor != 100 || len(first.Items) != 100 {
		t.Fatal("first page", err)
	}
	second, err := plan.ExportChildren(parent, first.NextCursor, 100)
	if err != nil || !second.Done || len(second.Items) != 20 {
		t.Fatal("second page", err)
	}
	seen := map[uuid.UUID]bool{}
	for _, child := range append(first.Items, second.Items...) {
		if seen[child.RunID] || child.Ordinal != child.SlotIndex/2 || child.Preset != presets[child.SlotIndex%2] {
			t.Fatal("duplicate or unordered child")
		}
		seen[child.RunID] = true
	}
}

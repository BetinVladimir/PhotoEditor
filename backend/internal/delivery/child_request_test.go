package delivery

import (
	"bytes"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestFrozenExportRequestBindsChildAndConfig(t *testing.T) {
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
	page, err := plan.ExportChildren(parent, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	child := page.Items[0]
	exportID, err := plan.NewChildExportID(parent, child)
	if err != nil || exportID == uuid.Nil || exportID == child.RunID {
		t.Fatal("new export identity", err)
	}
	replayedID, err := plan.NewChildExportID(parent, child)
	if err != nil || replayedID != exportID {
		t.Fatal("unstable export identity", err)
	}
	if _, err := plan.NewChildExportID(uuid.New(), child); err == nil {
		t.Fatal("foreign parent accepted for export identity")
	}
	request, first, err := plan.ExportRequest(parent, child)
	if err != nil || request.ConfigJCS != string(configs[0].Raw) {
		t.Fatal("frozen config", err)
	}
	configs[0].Raw[0] = 'x'
	_, again, err := plan.ExportRequest(parent, child)
	if err != nil || !bytes.Equal(first, again) {
		t.Fatal("mutable input affected request")
	}
	for _, kind := range []string{"run", "binding", "recipe", "preset", "ordinal", "slot", "digest"} {
		changed := child
		switch kind {
		case "run":
			changed.RunID = uuid.New()
		case "binding":
			changed.Binding.AssetVersionID = uuid.New()
		case "recipe":
			changed.Recipe.ArtifactID = uuid.New()
		case "preset":
			changed.Preset.PresetID = uuid.New()
		case "ordinal":
			changed.Ordinal++
		case "slot":
			changed.SlotIndex++
		case "digest":
			changed.SnapshotDigest = digest([]byte("other"))
		}
		if _, err := plan.NewChildExportID(parent, changed); err == nil {
			t.Fatal("export identity accepted substitution", kind)
		}
		if _, _, err := plan.ExportRequest(parent, changed); err == nil {
			t.Fatal("substitution accepted", kind)
		}
	}
}

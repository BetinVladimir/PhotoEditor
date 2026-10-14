package delivery

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestObservedMatrixUsesFrozenChildAndExportIdentity(t *testing.T) {
	plan, lease, _ := expansionPlanFixture(t)
	page, err := plan.ExportChildren(lease.Run, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	observations := make([]ChildObservation, len(page.Items))
	for index, child := range page.Items {
		export, err := plan.NewChildExportID(lease.Run, child)
		if err != nil {
			t.Fatal(err)
		}
		artifact := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest([]byte("raster"))}
		observations[index] = ChildObservation{SlotIndex: child.SlotIndex, Export: ExportObservation{ExportID: export,
			SnapshotDigest: child.SnapshotDigest, Binding: child.Binding, PresetSHA: child.Preset.PresetSHA,
			RunState: "completed", ExportState: "ready", ArtifactReady: true, Artifact: &artifact}}
	}
	slots, state, err := plan.ObserveMatrix(context.Background(), lease.Run, observations)
	if err != nil || state != "assemble" || len(slots) != len(observations) {
		t.Fatal(state, err)
	}
	slots[0].Export.SHA256 = "mutated"
	if observations[0].Export.Artifact.SHA256 == "mutated" {
		t.Fatal("borrowed artifact")
	}
	for _, scenario := range []string{"partial", "slot", "export", "binding", "pending", "failed"} {
		t.Run(scenario, func(t *testing.T) {
			input := append([]ChildObservation(nil), observations...)
			switch scenario {
			case "partial":
				input = input[:len(input)-1]
			case "slot":
				input[0].SlotIndex++
			case "export":
				input[0].Export.ExportID = uuid.New()
			case "binding":
				input[0].Export.Binding.AssetVersionID = uuid.New()
			case "pending":
				input[0].Export.RunState = "running"
			case "failed":
				input[0].Export.RunState = "failed"
				input[0].Export.ExportState = "failed"
			}
			calls := 0
			_, _, _, err := plan.BuildObservedAssemblyInput(context.Background(), lease.Run, input, func(context.Context, string, []byte) (v2input.Ref, error) { calls++; return v2input.Ref{}, nil })
			if err == nil || calls != 0 {
				t.Fatal("invalid matrix published", scenario, err, calls)
			}
		})
	}
}

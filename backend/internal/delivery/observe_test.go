package delivery

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func observeFixture() (SlotExpectation, ExportObservation, Policy) {
	source := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest([]byte("source"))}
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: source, CanvasHash: digest([]byte("canvas"))}
	expected := SlotExpectation{SnapshotDigest: digest([]byte("snapshot")), Binding: binding, Preset: FrozenPreset{PresetID: uuid.New(), PresetSHA: digest([]byte("preset")), Required: true}, ExportID: uuid.New()}
	artifact := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest([]byte("encoded"))}
	observed := ExportObservation{ExportID: expected.ExportID, SnapshotDigest: expected.SnapshotDigest, Binding: binding, PresetSHA: expected.Preset.PresetSHA, RunState: "completed", ExportState: "ready", ArtifactReady: true, Artifact: &artifact}
	return expected, observed, Policy{RequiredPresets: []uuid.UUID{expected.Preset.PresetID}, OptionalPresets: []uuid.UUID{}, Provenance: "off", AllowOmitted: true}
}
func TestObserveExportReadyOwnedAndPending(t *testing.T) {
	expected, observed, policy := observeFixture()
	slot, err := ObserveExport(expected, observed, policy)
	if err != nil || slot.State != "ready" {
		t.Fatal(slot, err)
	}
	observed.Artifact.SHA256 = digest([]byte("changed"))
	if slot.Export.SHA256 == observed.Artifact.SHA256 {
		t.Fatal("borrowed artifact")
	}
	observed.RunState = "running"
	slot, err = ObserveExport(expected, observed, policy)
	if err != nil || slot.State != "pending" || slot.Export != nil {
		t.Fatal(slot, err)
	}
}
func TestObserveExportRequiredAndOptionalFailure(t *testing.T) {
	for _, required := range []bool{true, false} {
		for _, allow := range []bool{true, false} {
			expected, observed, policy := observeFixture()
			expected.Preset.Required = required
			if !required {
				policy.RequiredPresets = []uuid.UUID{uuid.New()}
				policy.OptionalPresets = []uuid.UUID{expected.Preset.PresetID}
			}
			policy.AllowOmitted = allow
			observed.RunState = "failed"
			observed.Reason = "secret https://storage/locator"
			slot, err := ObserveExport(expected, observed, policy)
			want := "failed"
			if !required && allow {
				want = "omitted"
			}
			if err != nil || slot.State != want || slot.Export != nil || slot.ExportID != nil || slot.Reason == nil || *slot.Reason != "EXPORT_FAILED" {
				t.Fatal(slot, err)
			}
		}
	}
}
func TestObserveExportRefusesChangedBindingAndProvenance(t *testing.T) {
	expected, observed, policy := observeFixture()
	observed.Binding.AssetVersionID = uuid.New()
	if _, err := ObserveExport(expected, observed, policy); err == nil {
		t.Fatal("foreign source accepted")
	}
	expected, observed, policy = observeFixture()
	policy.Provenance = "required"
	slot, err := ObserveExport(expected, observed, policy)
	if err != nil || slot.State != "ready" || slot.ExportID == nil {
		t.Fatal("ready encoded export rejected before separate signing", err)
	}
	policy.Provenance = "unknown"
	if _, err := ObserveExport(expected, observed, policy); err == nil {
		t.Fatal("unknown provenance accepted")
	}
}
func TestMatrixStateDoesNotAssemblePendingOrFailed(t *testing.T) {
	for _, test := range []struct {
		slots []Slot
		want  string
	}{{[]Slot{{State: "ready"}, {State: "pending"}}, "pending"}, {[]Slot{{State: "failed"}, {State: "pending"}}, "failed"}, {[]Slot{{State: "ready"}, {State: "omitted"}}, "assemble"}, {[]Slot{{State: "omitted", Required: true}}, "failed"}} {
		state, err := MatrixState(test.slots)
		if err != nil || state != test.want {
			t.Fatal(state, err)
		}
	}
}

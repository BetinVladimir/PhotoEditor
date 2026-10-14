package delivery

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestManifestMatchesExactFrozenAssembly(t *testing.T) {
	plan, lease, _ := expansionPlanFixture(t)
	page, err := plan.ExportChildren(lease.Run, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	child := page.Items[0]
	export, err := plan.NewChildExportID(lease.Run, child)
	if err != nil {
		t.Fatal(err)
	}
	raster := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest([]byte("encoded"))}
	observations := []ChildObservation{{SlotIndex: 0, Export: ExportObservation{ExportID: export, SnapshotDigest: child.SnapshotDigest, Binding: child.Binding, PresetSHA: child.Preset.PresetSHA, RunState: "completed", ExportState: "ready", ArtifactReady: true, Artifact: &raster}}}
	var shards [][]byte
	input, rootRaw, sha, err := plan.BuildObservedAssemblyInput(context.Background(), lease.Run, observations, func(_ context.Context, _ string, raw []byte) (v2input.Ref, error) {
		shards = append(shards, append([]byte(nil), raw...))
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(raw)}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	root := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	for _, scenario := range []string{"valid", "count", "assembly", "shards", "unknown", "noncanonical", "hash", "corrupt_source", "foreign_parent", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			manifest := Manifest{SchemaVersion: 1, AssemblySHA: sha, SnapshotDigest: input.SnapshotDigest, SlotCount: 1, ReadyCount: 1, Shards: append([]v2input.Ref(nil), input.Shards...)}
			switch scenario {
			case "count":
				manifest.ReadyCount = 2
			case "assembly":
				manifest.AssemblySHA = digest([]byte("foreign"))
			case "shards":
				manifest.Shards = nil
			}
			raw, err := canonical.Marshal(manifest)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "unknown" {
				raw = append([]byte(`{"unknown":1,`), raw[1:]...)
			}
			if scenario == "noncanonical" {
				raw = append(raw, ' ')
			}
			result := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(raw)}
			if scenario == "hash" {
				result.SHA256 = digest([]byte("wrong"))
			}
			source := rootRaw
			parent := lease.Run
			if scenario == "corrupt_source" {
				source = append(append([]byte(nil), source...), ' ')
			}
			if scenario == "foreign_parent" {
				parent = uuid.New()
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancel" {
				cancel()
			}
			got, err := plan.ValidateManifest(ctx, parent, root, source, shards, result, raw)
			if scenario == "valid" {
				if err != nil || got.ReadyCount != 1 {
					t.Fatal(got, err)
				}
			} else if err == nil {
				t.Fatal("forged manifest accepted")
			}
		})
	}
}

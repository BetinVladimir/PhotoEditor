package v2input

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestCompileSessionDescribeClosureDeclaresPixelsAndAlpha(t *testing.T) {
	workflow, run := uuid.New(), uuid.New()
	plan, err := CompileSessionDescriptors(workflow, 1)
	if err != nil {
		t.Fatal(err)
	}
	image := Ref{uuid.New(), strings.Repeat("a", 64)}
	recipe := Ref{uuid.New(), strings.Repeat("b", 64)}
	pixels := Ref{uuid.New(), strings.Repeat("c", 64)}
	alpha := Ref{uuid.New(), strings.Repeat("d", 64)}
	items := []SessionSnapshotItem{{Ordinal: 0,
		Binding: SessionVersionBinding{AssetVersionID: uuid.New(),
			Source: image, CanvasHash: strings.Repeat("e", 64)},
		WorkingImage: image, Recipe: recipe}}
	snapshot, _, err := CompileSessionInputSnapshot(run, items)
	if err != nil {
		t.Fatal(err)
	}
	snapshotRef := Ref{uuid.New(), strings.Repeat("1", 64)}
	consents := Ref{uuid.New(), strings.Repeat("2", 64)}
	calibration := Ref{uuid.New(), strings.Repeat("3", 64)}
	params := Ref{uuid.New(), strings.Repeat("4", 64)}
	profile := json.RawMessage(`{"backend":"cuda","renderer_version":"v2.2","resource_estimate":{"ram_bytes":2147483648,"vram_bytes":6442450944,"scratch_bytes":1073741824,"output_bytes":8388608,"pixel_count":0}}`)
	resolved := []SessionDescribeImageInputs{{Ordinal: 0, Pixels: pixels, Alpha: &alpha}}
	closure, err := CompileSessionDescribeClosure(workflow, run, plan.Tasks[0],
		snapshot, resolved, snapshotRef, consents, calibration, params,
		strings.Repeat("5", 64), profile)
	if err != nil || closure.Open == nil || !digestPattern.MatchString(closure.InputDigest) ||
		len(closure.Direct) != 4 {
		t.Fatalf("invalid closure: %+v %v", closure, err)
	}
	cursor, err := closure.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	seen := map[uuid.UUID]string{}
	for {
		entry, ok, err := cursor.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		seen[entry.Ref.ArtifactID] = entry.Contract
	}
	if len(seen) != 8 || seen[pixels.ArtifactID] != "NpyArray" ||
		seen[alpha.ArtifactID] != "NpyArray" ||
		seen[image.ArtifactID] != "WorkingImage" ||
		seen[recipe.ArtifactID] != "EditingRecipeV3" {
		t.Fatalf("missing transitive image inputs: %v", seen)
	}
	resolved[0].Pixels = Ref{uuid.Nil, strings.Repeat("c", 64)}
	if _, err := CompileSessionDescribeClosure(workflow, run, plan.Tasks[0],
		snapshot, resolved, snapshotRef, consents, calibration, params,
		strings.Repeat("5", 64), profile); err == nil {
		t.Fatal("accepted missing pixels")
	}
	snapshot.SnapshotDigest = strings.Repeat("0", 64)
	resolved[0].Pixels = pixels
	if _, err := CompileSessionDescribeClosure(workflow, run, plan.Tasks[0],
		snapshot, resolved, snapshotRef, consents, calibration, params,
		strings.Repeat("5", 64), profile); err == nil {
		t.Fatal("accepted altered snapshot digest")
	}
}

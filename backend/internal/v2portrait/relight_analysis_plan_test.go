package v2portrait

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func analysisPlanFixture(t *testing.T) RelightAnalysisPlanRequest {
	t.Helper()
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(raw, &images) != nil {
		t.Fatal("fixture")
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	source, err := canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(source)
	canvas, _ := canonical.Hash(image.Canvas)
	pixels := image.Canvas.Width * image.Canvas.Height
	cost := LocalResourceEstimate{PixelCount: pixels, RAMBytes: 1 << 30, ScratchBytes: 32*pixels + (32 << 20) + (1 << 20), OutputBytes: 8*pixels + 65536}
	return RelightAnalysisPlanRequest{Tenant: uuid.New(), Project: uuid.New(), Actor: uuid.New(), Session: uuid.New(), Snapshot: uuid.New(), Key: "analysis-key", Operation: "depth.predict", Base: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, CanvasHash: canvas}, SourceRaw: source, Runtime: RelightAnalysisRuntime{Backend: "cpu", ModelLockSHA: strings.Repeat("a", 64), RuntimeLockSHA: strings.Repeat("b", 64), DeviceProfileSHA: strings.Repeat("c", 64), Qualification: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}, Resources: cost}, Capacity: cost}
}
func TestAnalysisPlanStableIdentityOwnedInputsAndChangedRequestBinding(t *testing.T) {
	request := analysisPlanFixture(t)
	first, err := CompileRelightAnalysisPlan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := CompileRelightAnalysisPlan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	a, b := first.Snapshot(), second.Snapshot()
	if a.Run != b.Run || a.Task != b.Task || string(a.Config) != string(b.Config) {
		t.Fatal("unstable identity")
	}
	a.Params[0] = 'x'
	a.Inputs[0].Contract = "changed"
	if first.Snapshot().Params[0] == 'x' || first.Snapshot().Inputs[0].Contract == "changed" {
		t.Fatal("borrowed output")
	}
	request.Runtime.Resources.RAMBytes++
	request.Capacity.RAMBytes++
	changed, err := CompileRelightAnalysisPlan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if changed.Snapshot().Run != b.Run || string(changed.Snapshot().Config) == string(b.Config) {
		t.Fatal("request conflict cannot be detected")
	}
	request.Operation = "mask.person"
	person, err := CompileRelightAnalysisPlan(context.Background(), request)
	if err != nil || person.Snapshot().Run == b.Run {
		t.Fatal("operation namespace", err)
	}
}
func TestAnalysisPlanRefusesCapacityAndMetalOverTenGB(t *testing.T) {
	request := analysisPlanFixture(t)
	request.Capacity.RAMBytes--
	if _, err := CompileRelightAnalysisPlan(context.Background(), request); err == nil {
		t.Fatal("under capacity")
	}
	request = analysisPlanFixture(t)
	request.Runtime.Backend = "mps"
	request.Runtime.Resources.VRAMBytes = 10000000001
	request.Capacity.VRAMBytes = 10000000001
	if _, err := CompileRelightAnalysisPlan(context.Background(), request); err == nil {
		t.Fatal("Metal ceiling")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CompileRelightAnalysisPlan(ctx, request); err == nil {
		t.Fatal("cancellation")
	}
}

func TestAnalysisPlanRetainsOriginalSourceBytes(t *testing.T) {
	request := analysisPlanFixture(t)
	original := string(request.SourceRaw)
	plan, err := CompileRelightAnalysisPlan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	request.SourceRaw[0] = 'x'
	if plan.Snapshot().SourceJCS != original {
		t.Fatal("source witness changed after compilation")
	}
	if _, err := plan.DraftTask(context.Background()); err != nil {
		t.Fatal(err)
	}
	changed := plan.Snapshot()
	changed.SourceJCS = "{}"
	if _, err := (RelightAnalysisPlan{snapshot: changed}).DraftTask(context.Background()); err == nil {
		t.Fatal("changed original source accepted")
	}
}

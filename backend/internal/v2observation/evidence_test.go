package v2observation

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestResolveFaceID(t *testing.T) {
	version, source, observation, face := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	binding := Binding{AssetVersionID: version, Source: ArtifactRef{ArtifactID: source, SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)}
	points := make([]map[string]float64, 68)
	for i := range points {
		points[i] = map[string]float64{"x": .5, "y": .5}
	}
	value := map[string]any{"schema_version": 1, "binding": binding,
		"faces": []any{map[string]any{"observation_id": observation, "face_id": face,
			"person_id": nil, "box": map[string]float64{"x0": 0, "y0": 0, "x1": 1, "y1": 1},
			"landmarks": points, "embedding": nil, "quality": .9}}}
	check := func(v any, expected Binding, observation uuid.UUID) (uuid.UUID, error) {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		return ResolveFaceID(raw, hex.EncodeToString(sum[:]), expected, observation)
	}
	got, err := check(value, binding, observation)
	if err != nil || got != face {
		t.Fatalf("valid evidence: %v %v", got, err)
	}
	if _, err := check(value, binding, uuid.New()); err == nil {
		t.Fatal("accepted absent observation")
	}
	badBinding := binding
	badBinding.AssetVersionID = uuid.New()
	if _, err := check(value, badBinding, observation); err == nil {
		t.Fatal("accepted other source version")
	}
	raw, _ := json.Marshal(value)
	if _, err := ResolveFaceID(raw, strings.Repeat("0", 64), binding, observation); err == nil {
		t.Fatal("accepted digest mismatch")
	}
	faceValue := value["faces"].([]any)[0].(map[string]any)
	faceValue["person_id"] = uuid.New()
	if _, err := check(value, binding, observation); err == nil {
		t.Fatal("accepted worker-assigned person")
	}
	faceValue["person_id"] = nil
	delete(faceValue, "embedding")
	if _, err := check(value, binding, observation); err == nil {
		t.Fatal("accepted missing required embedding field")
	}
	faceValue["embedding"] = nil
	value["faces"] = append(value["faces"].([]any), faceValue)
	if _, err := check(value, binding, observation); err == nil {
		t.Fatal("accepted duplicate face")
	}
	duplicate := []byte(`{"schema_version":1,"schema_version":1}`)
	sum := sha256.Sum256(duplicate)
	if _, err := ResolveFaceID(duplicate, hex.EncodeToString(sum[:]), binding, observation); err == nil {
		t.Fatal("accepted duplicate JSON key")
	}
}

func TestVerifyWorkingImageBinding(t *testing.T) {
	canvas := Canvas{Width: 200, Height: 100, Orientation: 1,
		SpaceID: "capture", SourceSHA256: strings.Repeat("c", 64)}
	canvasHash, err := canonical.Hash(canvas)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := canonical.Marshal(map[string]any{"pixels": map[string]any{
		"artifact_id": uuid.New(), "sha256": strings.Repeat("d", 64)},
		"canvas": canvas, "dtype": "float32"})
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	binding := Binding{AssetVersionID: uuid.New(), Source: ArtifactRef{
		ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, CanvasHash: canvasHash}
	if err := VerifyWorkingImageBinding(raw, binding, canvas.SourceSHA256); err != nil {
		t.Fatal(err)
	}
	if err := VerifyWorkingImageBinding(raw, binding, strings.Repeat("e", 64)); err == nil {
		t.Fatal("accepted different source lineage")
	}
	binding.CanvasHash = strings.Repeat("0", 64)
	if err := VerifyWorkingImageBinding(raw, binding, canvas.SourceSHA256); err == nil {
		t.Fatal("accepted wrong canvas hash")
	}
}

package v2optical

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestNegativeDenseManifestOccurrences(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []map[string]any
	if json.Unmarshal(raw, &images) != nil {
		t.Fatal("fixture")
	}
	paramsRaw, err := os.ReadFile("testdata/negative-params.json")
	if err != nil {
		t.Fatal(err)
	}
	original, err := ParseNegativeParams(paramsRaw)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"single", "duplicate", "conflict", "alias"} {
		var image map[string]any
		encoded, _ := json.Marshal(images[2])
		if json.Unmarshal(encoded, &image) != nil {
			t.Fatal("image")
		}
		image["color"] = map[string]any{"profile_id": "linear_srgb_v1", "encoding": "linear", "icc_sha256": strings.Repeat("c", 64)}
		chain := image["transform_chain"].(map[string]any)
		steps := chain["steps"].([]any)
		if mode != "single" {
			secondRaw, _ := json.Marshal(steps[0])
			var second map[string]any
			json.Unmarshal(secondRaw, &second)
			if mode == "conflict" {
				second["inverse_field"].(map[string]any)["sha256"] = strings.Repeat("a", 64)
			}
			if mode == "alias" {
				second["inverse_field"] = map[string]any{"artifact_id": original.Calibration.ArtifactID.String(), "sha256": original.Calibration.SHA256}
			}
			steps = append(steps, second)
			chain["steps"] = steps
		}
		chain["hash"], err = canonical.Hash(map[string]any{"steps": steps})
		if err != nil {
			t.Fatal(err)
		}
		source, err := canonical.Marshal(image)
		if err != nil {
			t.Fatal(err)
		}
		params := original
		sum := sha256.Sum256(source)
		params.Base.Source.SHA256 = hex.EncodeToString(sum[:])
		params.Base.CanvasHash, err = canonical.Hash(image["canvas"])
		if err != nil {
			t.Fatal(err)
		}
		entries, edges, err := NegativeImageInputs(params, source, v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)})
		if mode == "conflict" || mode == "alias" {
			if err == nil {
				t.Fatal(mode)
			}
			continue
		}
		expected := 2
		if mode == "duplicate" {
			expected = 3
		}
		if err != nil || len(entries) != 2 || len(edges) != expected {
			t.Fatal(mode, entries, edges, err)
		}
		for i, edge := range edges {
			if edge.Parent != params.Base.Source || edge.Binding != params.Base {
				t.Fatal(edge)
			}
			if i > 0 && !strings.HasPrefix(edge.Role, "InverseField/") {
				t.Fatal(edge)
			}
		}
	}
}

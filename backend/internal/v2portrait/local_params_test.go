package v2portrait

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"math"
	"os"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestCompiledParamsMatchDocumentedFields(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/implementation-v2/contracts/schemas.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Defs map[string]struct {
			Required []string `json:"required"`
		} `json:"$defs"`
	}
	if err = json.Unmarshal(raw, &spec); err != nil {
		t.Fatal(err)
	}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(make([]byte, 32))}
	base := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref, CanvasHash: ref.SHA256}
	hair := HairParams{SchemaVersion: 1, Base: base, Mask: ref, Protected: ref, SaturationFactor: 1}
	clothing := ClothingParams{SchemaVersion: 1, Base: base, Mask: ref, Protected: ref, SaturationFactor: 1}
	relight := RelightParams{SchemaVersion: 1, Base: base, Depth: ref, Subject: ref, Protected: ref, Calibration: ref, Lights: []RelightLight{{Radius: .5}}}
	for _, entry := range []struct {
		Name    string
		Compile func() ([]byte, string, error)
	}{{"PortraitHairParams", hair.Compile}, {"PortraitClothingParams", clothing.Compile}, {"ImageRelightParams", relight.Compile}} {
		body, digest, err := entry.Compile()
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != digest {
			t.Fatal("Params bytes/hash mismatch")
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(body, &fields) != nil {
			t.Fatal("invalid JSON")
		}
		required := spec.Defs[entry.Name].Required
		if len(fields) != len(required) || len(required) == 0 {
			t.Fatal("schema field mismatch")
		}
		for _, key := range required {
			if _, ok := fields[key]; !ok {
				t.Fatal("missing", key)
			}
		}
	}
	hair.Detail = math.NaN()
	if body, hash, err := hair.Compile(); err == nil || body != nil || hash != "" {
		t.Fatal("nonfinite controls produced Params")
	}
	clothing.Base.Source.ArtifactID = uuid.Nil
	if body, hash, err := clothing.Compile(); err == nil || body != nil || hash != "" {
		t.Fatal("missing source produced Params")
	}
}

func TestCompiledNumericBoundsFollowSchema(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/implementation-v2/contracts/schemas.json")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		Defs map[string]struct {
			Properties map[string]struct {
				Minimum float64 `json:"minimum"`
				Maximum float64 `json:"maximum"`
			} `json:"properties"`
		} `json:"$defs"`
	}
	if json.Unmarshal(raw, &spec) != nil {
		t.Fatal("invalid schema")
	}
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(make([]byte, 32))}
	base := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref, CanvasHash: ref.SHA256}
	for _, name := range []string{"PortraitHairParams", "PortraitClothingParams"} {
		fields := []string{"strength", "hue_delta_degrees", "saturation_factor"}
		if name == "PortraitHairParams" {
			fields = append(fields, "detail")
		} else {
			fields = append(fields, "wrinkle_strength")
		}
		for _, field := range fields {
			limits := spec.Defs[name].Properties[field]
			for _, value := range []float64{limits.Minimum, limits.Maximum, limits.Minimum - .01, limits.Maximum + .01, math.Inf(1), math.NaN()} {
				hair := HairParams{SchemaVersion: 1, Base: base, Mask: ref, Protected: ref, SaturationFactor: 1}
				clothing := ClothingParams{SchemaVersion: 1, Base: base, Mask: ref, Protected: ref, SaturationFactor: 1}
				switch field {
				case "strength":
					hair.Strength = value
					clothing.Strength = value
				case "hue_delta_degrees":
					hair.HueDeltaDegrees = value
					clothing.HueDeltaDegrees = value
				case "saturation_factor":
					hair.SaturationFactor = value
					clothing.SaturationFactor = value
				case "detail":
					hair.Detail = value
				case "wrinkle_strength":
					clothing.WrinkleStrength = value
				}
				var body []byte
				var sha string
				var err error
				if name == "PortraitHairParams" {
					body, sha, err = hair.Compile()
				} else {
					body, sha, err = clothing.Compile()
				}
				expected := !math.IsNaN(value) && !math.IsInf(value, 0) && value >= limits.Minimum && value <= limits.Maximum
				if (err == nil) != expected {
					t.Fatal(name, field, value, "unexpected acceptance", err)
				}
				if !expected && (body != nil || sha != "") {
					t.Fatal("invalid controls produced payload")
				}
			}
		}
	}
}

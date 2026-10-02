package v2draft

import (
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestScene12PredictionStandardizesAndClamps(t *testing.T) {
	frame := FrameFeatures{Binding: v2input.SessionVersionBinding{
		AssetVersionID: uuid.New(),
		Source:         v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)},
		CanvasHash:     strings.Repeat("b", 64)},
		LumaQuantiles:    [5]float64{.1, .2, .5, .7, .9},
		ClippingFraction: .02, Sharpness: 3, PersonFraction: .25}
	features, err := Scene12Features(frame)
	if err != nil || features[0] != .1 || features[5] != 0 || features[8] != 0 ||
		features[9] != .02 || features[10] != .75 || features[11] != .25 {
		t.Fatal("scene features", features, err)
	}
	neutral := [3]float64{.5, 1, 0}
	frame.NeutralRGB = &neutral
	features, err = Scene12Features(frame)
	if err != nil || math.Abs(features[5]-math.Log(.5)) > 1e-12 ||
		features[6] != 0 || math.Abs(features[7]-math.Log(1e-6)) > 1e-12 ||
		features[8] != 1 {
		t.Fatal("neutral features", features, err)
	}
	profile := CandidateProfile{SchemaVersion: 1, ProfileID: uuid.New(),
		TrainingSHA: strings.Repeat("c", 64), FeatureOrder: "scene12_v1", RidgeLambda: .1}
	for i := range profile.Scales {
		profile.Scales[i] = 1
	}
	profile.Coefficients[0][0] = 20
	profile.Intercept[1] = .5
	predicted, err := PredictCorrection(profile, frame)
	if err != nil || predicted.EV != 1 || predicted.LogR != .223144 ||
		predicted.LogG != 0 {
		t.Fatal("prediction", predicted, err)
	}
	profile.TrainingSHA = strings.Repeat("0", 64)
	if _, err = PredictCorrection(profile, frame); err == nil {
		t.Fatal("accepted nonzero builtin profile")
	}
	profile.Coefficients[0][0], profile.Intercept[1] = 0, 0
	if zero, e := PredictCorrection(profile, frame); e != nil || zero != (CorrectionVector{}) {
		t.Fatal("builtin zero profile", zero, e)
	}
	body, err := canonical.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeCandidateProfile(body); err != nil {
		t.Fatal("valid profile decode", err)
	}
	var fields map[string]any
	if err = json.Unmarshal(body, &fields); err != nil {
		t.Fatal(err)
	}
	fields["coefficients"].([]any)[0] = []any{0, 0}
	short, _ := json.Marshal(fields)
	if _, err = DecodeCandidateProfile(short); err == nil {
		t.Fatal("accepted truncated coefficient row")
	}
	descriptor := map[string]any{
		"schema_version": 1, "binding": frame.Binding, "ordinal": 0,
		"captured_at": nil, "camera_hash": nil,
		"scene_embedding": make([]float64, 384),
		"luma_quantiles":  []float64{.1, .2, .5, .7, .9},
		"neutral_rgb":     []float64{.5, 1, 0},
		"sharpness":       3, "clipping_fraction": .02, "person_fraction": .25,
		"faces": v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)},
	}
	descriptorBytes, err := canonical.Marshal(descriptor)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeFrameFeatures(descriptorBytes, frame.Binding)
	if err != nil || decoded.NeutralRGB == nil || *decoded.NeutralRGB != neutral ||
		decoded.Faces != descriptor["faces"].(v2input.Ref) {
		t.Fatal("descriptor decode", decoded, err)
	}
	for _, name := range []string{"ordinal", "sharpness", "clipping_fraction", "person_fraction", "scene_embedding", "luma_quantiles", "neutral_rgb"} {
		var mutated map[string]any
		if json.Unmarshal(descriptorBytes, &mutated) != nil {
			t.Fatal("fixture")
		}
		if values, ok := mutated[name].([]any); ok {
			values[0] = nil
		} else {
			mutated[name] = nil
		}
		bad, _ := canonical.Marshal(mutated)
		if _, err := DecodeFrameFeatures(bad, frame.Binding); err == nil {
			t.Fatal("null feature accepted", name)
		}
	}
	descriptor["scene_embedding"] = make([]float64, 383)
	short, _ = json.Marshal(descriptor)
	if _, err = DecodeFrameFeatures(short, frame.Binding); err == nil {
		t.Fatal("accepted truncated scene embedding")
	}
	frame.LumaQuantiles[1] = .05
	if _, err = PredictCorrection(profile, frame); err == nil {
		t.Fatal("accepted unsorted quantiles")
	}
}

func TestCandidateProfileRejectsNullNumericParameters(t *testing.T) {
	profile := CandidateProfile{SchemaVersion: 1, ProfileID: uuid.New(), TrainingSHA: strings.Repeat("c", 64), FeatureOrder: "scene12_v1", RidgeLambda: .1}
	for i := range profile.Scales {
		profile.Scales[i] = 1
	}
	raw, _ := canonical.Marshal(profile)
	if _, err := DecodeCandidateProfile(raw); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"means", "scales", "coefficients", "intercept"} {
		var doc map[string]any
		if json.Unmarshal(raw, &doc) != nil {
			t.Fatal("fixture")
		}
		values := doc[field].([]any)
		if field == "coefficients" {
			values[0].([]any)[0] = nil
		} else {
			values[0] = nil
		}
		bad, _ := canonical.Marshal(doc)
		if _, err := DecodeCandidateProfile(bad); err == nil {
			t.Fatal("null numeric accepted", field)
		}
	}
}

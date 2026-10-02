package v2draft

import (
	"bytes"
	"encoding/json"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func finiteJSONNumber(raw json.RawMessage) bool {
	var value float64
	return !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) && json.Unmarshal(raw, &value) == nil && !math.IsNaN(value) && !math.IsInf(value, 0)
}

// CandidateProfile is the immutable scene12_v1 linear model. Provenance of a
// non-builtin TrainingManifest is checked by the caller before prediction.
type CandidateProfile struct {
	SchemaVersion int            `json:"schema_version"`
	ProfileID     uuid.UUID      `json:"profile_id"`
	TrainingSHA   string         `json:"training_sha"`
	FeatureOrder  string         `json:"feature_order"`
	Means         [12]float64    `json:"means"`
	Scales        [12]float64    `json:"scales"`
	Coefficients  [12][6]float64 `json:"coefficients"`
	Intercept     [6]float64     `json:"intercept"`
	RidgeLambda   float64        `json:"ridge_lambda"`
}

type FrameFeatures struct {
	Binding          v2input.SessionVersionBinding
	Faces            v2input.Ref
	LumaQuantiles    [5]float64
	NeutralRGB       *[3]float64
	ClippingFraction float64
	Sharpness        float64
	PersonFraction   float64
}

// DecodeFrameFeatures validates the full FrameDescriptor wire shape before
// extracting the scalar inputs used by scene12_v1.
func DecodeFrameFeatures(raw []byte,
	binding v2input.SessionVersionBinding) (FrameFeatures, error) {
	fields, ok := exactFields(raw, "schema_version", "binding", "ordinal",
		"captured_at", "camera_hash", "scene_embedding", "luma_quantiles",
		"neutral_rgb", "sharpness", "clipping_fraction", "person_fraction", "faces")
	if !ok || len(raw) < 2 || len(raw) > maxDocumentJSON ||
		!exactFrameBinding(fields["binding"]) {
		return FrameFeatures{}, v2input.ErrInvalid
	}
	if _, ok = exactFields(fields["faces"], "artifact_id", "sha256"); !ok {
		return FrameFeatures{}, v2input.ErrInvalid
	}
	for _, name := range []string{"ordinal", "sharpness", "clipping_fraction", "person_fraction"} {
		if !finiteJSONNumber(fields[name]) {
			return FrameFeatures{}, v2input.ErrInvalid
		}
	}
	for _, name := range []string{"scene_embedding", "luma_quantiles", "neutral_rgb"} {
		if name == "neutral_rgb" && bytes.Equal(bytes.TrimSpace(fields[name]), []byte("null")) {
			continue
		}
		var elements []json.RawMessage
		if json.Unmarshal(fields[name], &elements) != nil || elements == nil {
			return FrameFeatures{}, v2input.ErrInvalid
		}
		for _, element := range elements {
			if !finiteJSONNumber(element) {
				return FrameFeatures{}, v2input.ErrInvalid
			}
		}
	}
	var doc struct {
		SchemaVersion    int                           `json:"schema_version"`
		Binding          v2input.SessionVersionBinding `json:"binding"`
		Ordinal          int                           `json:"ordinal"`
		CapturedAt       *string                       `json:"captured_at"`
		CameraHash       *string                       `json:"camera_hash"`
		SceneEmbedding   []float64                     `json:"scene_embedding"`
		LumaQuantiles    []float64                     `json:"luma_quantiles"`
		NeutralRGB       []float64                     `json:"neutral_rgb"`
		Sharpness        float64                       `json:"sharpness"`
		ClippingFraction float64                       `json:"clipping_fraction"`
		PersonFraction   float64                       `json:"person_fraction"`
		Faces            v2input.Ref                   `json:"faces"`
	}
	if json.Unmarshal(raw, &doc) != nil || doc.SchemaVersion != 1 ||
		doc.Binding != binding || doc.Ordinal < 0 || doc.Ordinal > 4999 ||
		len(doc.SceneEmbedding) != 384 || len(doc.LumaQuantiles) != 5 ||
		(doc.NeutralRGB != nil && len(doc.NeutralRGB) != 3) ||
		!isDocumentRef(doc.Faces) ||
		(doc.CameraHash != nil && !correctionDigest(*doc.CameraHash)) {
		return FrameFeatures{}, v2input.ErrInvalid
	}
	if doc.CapturedAt != nil {
		if _, err := time.Parse(time.RFC3339Nano, *doc.CapturedAt); err != nil {
			return FrameFeatures{}, v2input.ErrInvalid
		}
	}
	for _, value := range doc.SceneEmbedding {
		if !inRange(value, -1, 1) {
			return FrameFeatures{}, v2input.ErrInvalid
		}
	}
	frame := FrameFeatures{Binding: binding, Faces: doc.Faces,
		Sharpness:        doc.Sharpness,
		ClippingFraction: doc.ClippingFraction, PersonFraction: doc.PersonFraction}
	copy(frame.LumaQuantiles[:], doc.LumaQuantiles)
	if doc.NeutralRGB != nil {
		var neutral [3]float64
		copy(neutral[:], doc.NeutralRGB)
		frame.NeutralRGB = &neutral
	}
	if _, err := Scene12Features(frame); err != nil {
		return FrameFeatures{}, err
	}
	return frame, nil
}

func exactFrameBinding(raw []byte) bool {
	fields, ok := exactFields(raw, "asset_version_id", "source", "canvas_hash")
	if !ok {
		return false
	}
	_, ok = exactFields(fields["source"], "artifact_id", "sha256")
	return ok
}

// DecodeCandidateProfile enforces array dimensions that Go's fixed-array JSON
// decoder would otherwise silently pad with zeros.
func DecodeCandidateProfile(raw []byte) (CandidateProfile, error) {
	fields, ok := exactFields(raw, "schema_version", "profile_id",
		"training_sha", "feature_order", "means", "scales",
		"coefficients", "intercept", "ridge_lambda")
	if !ok || len(raw) < 2 || len(raw) > maxDocumentJSON {
		return CandidateProfile{}, v2input.ErrInvalid
	}
	for _, field := range []string{"means", "scales", "coefficients"} {
		var values []json.RawMessage
		if json.Unmarshal(fields[field], &values) != nil || len(values) != 12 {
			return CandidateProfile{}, v2input.ErrInvalid
		}
		if field != "coefficients" {
			for _, rawValue := range values {
				if !finiteJSONNumber(rawValue) {
					return CandidateProfile{}, v2input.ErrInvalid
				}
			}
		}
		if field == "coefficients" {
			for _, row := range values {
				var elements []json.RawMessage
				if json.Unmarshal(row, &elements) != nil || len(elements) != 6 {
					return CandidateProfile{}, v2input.ErrInvalid
				}
				for _, rawValue := range elements {
					if !finiteJSONNumber(rawValue) {
						return CandidateProfile{}, v2input.ErrInvalid
					}
				}
			}
		}
	}
	var intercept []json.RawMessage
	if json.Unmarshal(fields["intercept"], &intercept) != nil || len(intercept) != 6 {
		return CandidateProfile{}, v2input.ErrInvalid
	}
	for _, rawValue := range intercept {
		if !finiteJSONNumber(rawValue) {
			return CandidateProfile{}, v2input.ErrInvalid
		}
	}
	var profile CandidateProfile
	if json.Unmarshal(raw, &profile) != nil || !profile.valid() {
		return CandidateProfile{}, v2input.ErrInvalid
	}
	return profile, nil
}

func clamp(value, low, high float64) float64 {
	return math.Min(high, math.Max(low, value))
}

func (p CandidateProfile) valid() bool {
	if p.SchemaVersion != 1 || p.ProfileID == uuid.Nil ||
		!correctionDigest(p.TrainingSHA) || p.FeatureOrder != "scene12_v1" ||
		(p.RidgeLambda != .01 && p.RidgeLambda != .1 &&
			p.RidgeLambda != 1 && p.RidgeLambda != 10) {
		return false
	}
	builtin := p.TrainingSHA == strings.Repeat("0", 64)
	for i := range p.Means {
		if !inRange(p.Means[i], -100, 100) ||
			!inRange(p.Scales[i], 1e-8, 1e8) ||
			builtin && (p.Means[i] != 0 || p.Scales[i] != 1) {
			return false
		}
		for j := range p.Coefficients[i] {
			if !inRange(p.Coefficients[i][j], -1e6, 1e6) ||
				builtin && p.Coefficients[i][j] != 0 {
				return false
			}
		}
	}
	for _, value := range p.Intercept {
		if !inRange(value, -1e6, 1e6) || builtin && value != 0 {
			return false
		}
	}
	return true
}

// Scene12Features uses the frozen descriptor captured before any user edit.
// A missing neutral sample contributes zero logs and neutral_present=0.
func Scene12Features(frame FrameFeatures) ([12]float64, error) {
	var out [12]float64
	if frame.Binding.AssetVersionID == uuid.Nil ||
		!isDocumentRef(frame.Binding.Source) ||
		!correctionDigest(frame.Binding.CanvasHash) ||
		!inRange(frame.ClippingFraction, 0, 1) ||
		!inRange(frame.Sharpness, 0, 1e6) ||
		!inRange(frame.PersonFraction, 0, 1) {
		return out, v2input.ErrInvalid
	}
	for i, value := range frame.LumaQuantiles {
		if !inRange(value, 0, 1) || i > 0 && value < frame.LumaQuantiles[i-1] {
			return [12]float64{}, v2input.ErrInvalid
		}
		out[i] = value
	}
	if frame.NeutralRGB != nil {
		for i, value := range *frame.NeutralRGB {
			if !inRange(value, 0, 1) {
				return [12]float64{}, v2input.ErrInvalid
			}
			out[5+i] = math.Log(math.Max(value, 1e-6))
		}
		out[8] = 1
	}
	out[9] = frame.ClippingFraction
	out[10] = frame.Sharpness / (1 + frame.Sharpness)
	out[11] = frame.PersonFraction
	return out, nil
}

// PredictCorrection standardizes using this exact profile and clamps only
// inference output to CorrectionVector bounds, never authored corrections.
func PredictCorrection(profile CandidateProfile,
	frame FrameFeatures) (CorrectionVector, error) {
	if !profile.valid() {
		return CorrectionVector{}, v2input.ErrInvalid
	}
	features, err := Scene12Features(frame)
	if err != nil {
		return CorrectionVector{}, err
	}
	var values [6]float64
	for j := range values {
		values[j] = profile.Intercept[j]
		for i, feature := range features {
			values[j] += ((feature - profile.Means[i]) /
				profile.Scales[i]) * profile.Coefficients[i][j]
		}
		if math.IsNaN(values[j]) || math.IsInf(values[j], 0) {
			return CorrectionVector{}, v2input.ErrInvalid
		}
	}
	return CorrectionVector{
		EV:              clamp(values[0], -1, 1),
		LogR:            clamp(values[1], -.223144, .223144),
		LogG:            clamp(values[2], -.223144, .223144),
		LogB:            clamp(values[3], -.223144, .223144),
		SaturationDelta: clamp(values[4], -.15, .15),
		RetouchDelta:    clamp(values[5], -.15, .15),
	}, nil
}

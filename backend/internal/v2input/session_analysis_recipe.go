package v2input

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
)

type sessionCorrectionVector struct {
	EV              float64 `json:"ev"`
	LogR            float64 `json:"log_r"`
	LogG            float64 `json:"log_g"`
	LogB            float64 `json:"log_b"`
	SaturationDelta float64 `json:"saturation_delta"`
	RetouchDelta    float64 `json:"retouch_delta"`
}

type sessionRecipe struct {
	SchemaVersion      int                     `json:"schema_version"`
	Source             SessionVersionBinding   `json:"source"`
	BaseStyle          Ref                     `json:"base_style"`
	SessionCorrections *Ref                    `json:"session_corrections"`
	LearningVersionID  *uuid.UUID              `json:"learning_version_id"`
	Overrides          []Ref                   `json:"overrides"`
	LayerStack         *Ref                    `json:"layer_stack"`
	ProtectedMask      *Ref                    `json:"protected_mask,omitempty"`
	RendererVersion    string                  `json:"renderer_version"`
	ParameterDelta     sessionCorrectionVector `json:"parameter_delta"`
}

func exactJSONKeys(raw []byte, keys ...string) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return false
	}
	allowed := make(map[string]bool, len(keys))
	for _, key := range keys {
		allowed[key] = true
	}
	seen := make(map[string]bool, len(keys))
	for decoder.More() {
		token, err = decoder.Token()
		if err != nil {
			return false
		}
		key, ok := token.(string)
		if !ok || !allowed[key] || seen[key] {
			return false
		}
		seen[key] = true
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return false
		}
	}
	token, err = decoder.Token()
	return err == nil && token == json.Delim('}') && len(seen) == len(allowed) && decoder.Decode(new(any)) == io.EOF
}

func bounded(value, low, high float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= low && value <= high
}

func validateSessionRecipe(raw []byte, binding SessionVersionBinding) error {
	if !exactJSONKeys(raw, "schema_version", "source", "base_style",
		"session_corrections", "learning_version_id", "overrides",
		"layer_stack", "renderer_version", "parameter_delta") &&
		!exactJSONKeys(raw, "schema_version", "source", "base_style",
			"session_corrections", "learning_version_id", "overrides",
			"layer_stack", "protected_mask", "renderer_version",
			"parameter_delta") {
		return ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil ||
		!exactJSONKeys(fields["parameter_delta"], "ev", "log_r", "log_g",
			"log_b", "saturation_delta", "retouch_delta") ||
		len(fields["overrides"]) == 0 || fields["overrides"][0] != '[' {
		return ErrInvalid
	}
	var sourceFields map[string]json.RawMessage
	if !exactJSONKeys(fields["source"], "asset_version_id", "source", "canvas_hash") ||
		json.Unmarshal(fields["source"], &sourceFields) != nil ||
		!exactJSONKeys(sourceFields["source"], "artifact_id", "sha256") ||
		!exactJSONKeys(fields["base_style"], "artifact_id", "sha256") {
		return ErrInvalid
	}
	for _, name := range []string{"session_corrections", "layer_stack"} {
		if string(fields[name]) != "null" && !exactJSONKeys(fields[name], "artifact_id", "sha256") {
			return ErrInvalid
		}
	}
	if protected, present := fields["protected_mask"]; present &&
		!exactJSONKeys(protected, "artifact_id", "sha256") {
		return ErrInvalid
	}
	var overrideFields []json.RawMessage
	if json.Unmarshal(fields["overrides"], &overrideFields) != nil {
		return ErrInvalid
	}
	for _, item := range overrideFields {
		if !exactJSONKeys(item, "artifact_id", "sha256") {
			return ErrInvalid
		}
	}
	var doc sessionRecipe
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&doc) != nil || decoder.Decode(new(any)) != io.EOF ||
		doc.SchemaVersion != 3 || doc.RendererVersion != "v2.2" ||
		doc.Source != binding || doc.Source.AssetVersionID == uuid.Nil ||
		!validRef(doc.Source.Source) || !digestPattern.MatchString(doc.Source.CanvasHash) ||
		!validRef(doc.BaseStyle) ||
		len(doc.Overrides) > 128 ||
		(doc.SessionCorrections != nil && !validRef(*doc.SessionCorrections)) ||
		(doc.LayerStack != nil && !validRef(*doc.LayerStack)) ||
		(len(fields["protected_mask"]) != 0 &&
			(doc.ProtectedMask == nil || !validRef(*doc.ProtectedMask))) ||
		(doc.LearningVersionID != nil && *doc.LearningVersionID == uuid.Nil) ||
		!bounded(doc.ParameterDelta.EV, -1, 1) ||
		!bounded(doc.ParameterDelta.LogR, -0.223144, 0.223144) ||
		!bounded(doc.ParameterDelta.LogG, -0.223144, 0.223144) ||
		!bounded(doc.ParameterDelta.LogB, -0.223144, 0.223144) ||
		!bounded(doc.ParameterDelta.SaturationDelta, -0.15, 0.15) ||
		!bounded(doc.ParameterDelta.RetouchDelta, -0.15, 0.15) {
		return ErrInvalid
	}
	for _, ref := range doc.Overrides {
		if !validRef(ref) {
			return ErrInvalid
		}
	}
	return nil
}

// ValidateEditingRecipeV3 is shared by draft publication and session
// admission. Both paths must reject a recipe whose source binding or bounded
// parameter delta differs from the immutable asset version.
func ValidateEditingRecipeV3(raw []byte, binding SessionVersionBinding) error {
	return validateSessionRecipe(raw, binding)
}

// readSessionRecipe verifies the exact immutable object version and its
// source binding before its ArtifactRef is included in InputSnapshot.
func readSessionRecipe(ctx context.Context, objects SessionAnalysisObjectReader,
	object objectstore.Object, ref Ref, binding SessionVersionBinding) error {
	if objects == nil || !validRef(ref) || object.SHA256 != ref.SHA256 ||
		object.ContentType != "application/json" || object.Bytes < 2 ||
		object.Bytes > 1048576 || object.VersionID == "" || object.VersionID == "null" {
		return ErrInvalid
	}
	reader, err := objects.Open(ctx, object)
	if err != nil {
		return err
	}
	raw, readErr := io.ReadAll(io.LimitReader(reader, 1048577))
	closeErr := reader.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil || ctx.Err() != nil || int64(len(raw)) != object.Bytes {
		return ErrInvalid
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != ref.SHA256 {
		return ErrInvalid
	}
	canonicalBody, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(canonicalBody, raw) {
		return ErrInvalid
	}
	return validateSessionRecipe(raw, binding)
}

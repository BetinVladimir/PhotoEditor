package v2input

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/canonical"
	"unicode/utf8"
)

type SemanticTransferParams struct {
	SchemaVersion int                   `json:"schema_version"`
	Intent        Ref                   `json:"intent"`
	Reference     SessionVersionBinding `json:"reference"`
	Target        SessionVersionBinding `json:"target"`
	ModelLockSHA  string                `json:"model_lock_sha"`
	Calibration   Ref                   `json:"calibration"`
}

type SemanticEditIntent struct {
	SchemaVersion    int                   `json:"schema_version"`
	Kind             string                `json:"kind"`
	Reference        SessionVersionBinding `json:"reference"`
	ReferenceMask    Ref                   `json:"reference_mask"`
	TargetSnapshotID uuid.UUID             `json:"target_snapshot_id"`
	Strength         float64               `json:"strength"`
	EV               *float64              `json:"ev"`
	PreservePersons  bool                  `json:"preserve_persons"`
	Prompt           *string               `json:"prompt"`
}

func semanticObject(raw []byte, nullable map[string]bool, keys ...string) (map[string]json.RawMessage, bool) {
	if !exactJSONKeys(raw, keys...) {
		return nil, false
	}
	values := map[string]json.RawMessage{}
	if json.Unmarshal(raw, &values) != nil {
		return nil, false
	}
	for _, key := range keys {
		if !nullable[key] && bytes.Equal(bytes.TrimSpace(values[key]), []byte("null")) {
			return nil, false
		}
	}
	return values, true
}
func semanticRefJSON(raw []byte) bool {
	_, ok := semanticObject(raw, nil, "artifact_id", "sha256")
	return ok
}
func semanticBindingJSON(raw []byte) bool {
	values, ok := semanticObject(raw, nil, "asset_version_id", "source", "canvas_hash")
	return ok && semanticRefJSON(values["source"])
}
func semanticDecode(raw []byte, value any) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value) == nil && decoder.Decode(new(any)) == io.EOF
}
func semanticBindingValid(value SessionVersionBinding) bool {
	return value.AssetVersionID != uuid.Nil && validRef(value.Source) && digestPattern.MatchString(value.CanvasHash)
}

func DecodeSemanticTransferParams(raw []byte) (SemanticTransferParams, error) {
	var value SemanticTransferParams
	if len(raw) > 1048576 {
		return value, ErrInvalid
	}
	fields, ok := semanticObject(raw, nil, "schema_version", "intent", "reference", "target", "model_lock_sha", "calibration")
	if !ok || !semanticRefJSON(fields["intent"]) || !semanticRefJSON(fields["calibration"]) || !semanticBindingJSON(fields["reference"]) || !semanticBindingJSON(fields["target"]) || !semanticDecode(raw, &value) {
		return SemanticTransferParams{}, ErrInvalid
	}
	if value.SchemaVersion != 1 || !validRef(value.Intent) || !validRef(value.Calibration) || !semanticBindingValid(value.Reference) || !semanticBindingValid(value.Target) || !digestPattern.MatchString(value.ModelLockSHA) {
		return SemanticTransferParams{}, ErrInvalid
	}
	return value, nil
}

func DecodeSemanticEditIntent(raw []byte) (SemanticEditIntent, error) {
	var value SemanticEditIntent
	if len(raw) > 1048576 {
		return value, ErrInvalid
	}
	fields, ok := semanticObject(raw, map[string]bool{"ev": true, "prompt": true}, "schema_version", "kind", "reference", "reference_mask", "target_snapshot_id", "strength", "ev", "preserve_persons", "prompt")
	if !ok || !semanticBindingJSON(fields["reference"]) || !semanticRefJSON(fields["reference_mask"]) || !semanticDecode(raw, &value) {
		return SemanticEditIntent{}, ErrInvalid
	}
	kind := value.Kind == "remove_object" || value.Kind == "local_exposure" || value.Kind == "background_cleanup" || value.Kind == "retouch_region"
	if value.SchemaVersion != 1 || !kind || !semanticBindingValid(value.Reference) || !validRef(value.ReferenceMask) || value.TargetSnapshotID == uuid.Nil || !finiteRange(value.Strength, 0, 1) || !value.PreservePersons || (value.Kind == "local_exposure") != (value.EV != nil) {
		return SemanticEditIntent{}, ErrInvalid
	}
	if value.EV != nil && !finiteRange(*value.EV, -1, 1) {
		return SemanticEditIntent{}, ErrInvalid
	}
	if value.Prompt != nil && (!utf8.ValidString(*value.Prompt) || utf8.RuneCountInString(*value.Prompt) < 1 || utf8.RuneCountInString(*value.Prompt) > 1024) {
		return SemanticEditIntent{}, ErrInvalid
	}
	return value, nil
}

// BindSemanticTransferIntent owns and validates content pins; scoped source,
// target snapshot, calibration and qualified downstream admission are external.
func BindSemanticTransferIntent(params SemanticTransferParams, intent SemanticEditIntent) (SemanticTransferParams, SemanticEditIntent, error) {
	var p SemanticTransferParams
	var i SemanticEditIntent
	raw, err := canonical.Marshal(params)
	if err != nil {
		return p, i, ErrInvalid
	}
	p, err = DecodeSemanticTransferParams(raw)
	if err != nil {
		return SemanticTransferParams{}, i, err
	}
	raw, err = canonical.Marshal(intent)
	if err != nil {
		return SemanticTransferParams{}, i, ErrInvalid
	}
	i, err = DecodeSemanticEditIntent(raw)
	if err != nil {
		return SemanticTransferParams{}, SemanticEditIntent{}, err
	}
	digest, err := canonical.Hash(i)
	if err != nil || p.Intent.SHA256 != digest || p.Reference != i.Reference {
		return SemanticTransferParams{}, SemanticEditIntent{}, ErrInvalid
	}
	return p, i, nil
}

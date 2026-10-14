package v2draft

import (
	"bytes"
	"encoding/json"

	"github.com/google/uuid"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// LocalPortraitClientSelection contains editor choices only. Durable owner,
// runtime, resource estimate, policy and quota are bound by the service.
type LocalPortraitClientSelection struct {
	SchemaVersion  int             `json:"schema_version"`
	Snapshot       uuid.UUID       `json:"snapshot_id"`
	Ordinal        int             `json:"ordinal"`
	Operation      string          `json:"operation"`
	Params         json.RawMessage `json:"params"`
	TargetPixel    v2input.Ref     `json:"target_pixel"`
	ProtectedPixel v2input.Ref     `json:"protected_pixel"`
}

func DecodeLocalPortraitClientSelection(raw []byte) (LocalPortraitClientSelection, error) {
	var zero LocalPortraitClientSelection
	if len(raw) < 2 || len(raw) > 16384 {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normal, raw) {
		return zero, v2input.ErrInvalid
	}
	var request LocalPortraitClientSelection
	if json.Unmarshal(raw, &request) != nil || request.SchemaVersion != 1 || request.Snapshot == uuid.Nil || request.Ordinal < 0 || request.Ordinal >= 5000 || len(request.Params) < 2 || len(request.Params) > 8192 {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(request)
	if err != nil || !bytes.Equal(exact, raw) {
		return zero, v2input.ErrInvalid
	}
	var base v2input.SessionVersionBinding
	var mask, guard v2input.Ref
	var body []byte
	switch request.Operation {
	case "portrait.hair":
		var params v2portrait.HairParams
		if json.Unmarshal(request.Params, &params) != nil {
			return zero, v2input.ErrInvalid
		}
		body, _, err = params.Compile()
		base, mask, guard = params.Base, params.Mask, params.Protected
	case "portrait.clothing":
		var params v2portrait.ClothingParams
		if json.Unmarshal(request.Params, &params) != nil {
			return zero, v2input.ErrInvalid
		}
		body, _, err = params.Compile()
		base, mask, guard = params.Base, params.Mask, params.Protected
	default:
		return zero, v2input.ErrInvalid
	}
	if err != nil || !bytes.Equal(body, request.Params) || base.AssetVersionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, ref := range []v2input.Ref{base.Source, mask, guard, request.TargetPixel, request.ProtectedPixel} {
		if ref.ArtifactID == uuid.Nil || seen[ref.ArtifactID] {
			return zero, v2input.ErrInvalid
		}
		if _, err = domain.ParseDigest(ref.SHA256); err != nil {
			return zero, v2input.ErrInvalid
		}
		seen[ref.ArtifactID] = true
	}
	if _, err = domain.ParseDigest(base.CanvasHash); err != nil {
		return zero, v2input.ErrInvalid
	}
	return request, nil
}

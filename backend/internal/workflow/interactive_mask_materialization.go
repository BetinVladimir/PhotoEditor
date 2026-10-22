package workflow

import (
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"sort"
)

// InteractiveMaskMaterialization is pure compilation, not admission, a
// canonical result or permission to use a mask in a persistent override.
type InteractiveMaskMaterialization struct {
	Inputs  []review.ArtifactRef
	Digest  string
	Profile json.RawMessage
}

// CompileInteractiveMaskMaterialization shares the current interactive task
// profile/input contract with operator measurement. The caller must have
// independently validated prompt/manual Params and source bytes. The normal
// coordinator retains SQL claim, template validation, publication and fencing.
func CompileInteractiveMaskMaterialization(l InteractiveLease, source styleArtifact, image review.WorkingImage, params artifactjson.Ref) (InteractiveMaskMaterialization, error) {
	var result InteractiveMaskMaterialization
	if !((l.Kind == "mask_manual" && l.Operation == "mask.rasterize") || (l.Kind == "mask_prompt" && l.Operation == "mask.prompt")) {
		return result, errors.New("interactive mask operation mismatch")
	}
	if source.ArtifactID != l.SourceID || params.ID == uuid.Nil || len(params.SHA256) != 64 || image.Canvas.Width < 1 || image.Canvas.Height < 1 {
		return result, errors.New("interactive mask source invalid")
	}
	var inputs []review.ArtifactRef
	if json.Unmarshal(l.Declared, &inputs) != nil {
		return result, errors.New("invalid interactive inputs")
	}
	inputs = append(inputs, source.ref(), image.Pixels)
	if image.Alpha != nil {
		inputs = append(inputs, *image.Alpha)
	}
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].ID.String() < inputs[j].ID.String() })
	unique := inputs[:0]
	for _, ref := range inputs {
		if len(unique) == 0 || unique[len(unique)-1].ID != ref.ID {
			unique = append(unique, ref)
		} else if unique[len(unique)-1] != ref {
			return result, errors.New("interactive artifact identity conflict")
		}
	}
	digest, err := repairInputDigest(l.Operation, "1", review.ArtifactRef{ID: params.ID, SHA256: params.SHA256}, unique)
	if err != nil {
		return result, err
	}
	pixels := image.Canvas.Width * image.Canvas.Height
	out := pixels*16 + 8*1048576
	profile, err := canonical.Marshal(map[string]any{"backend": l.Backend, "precision": "float32", "tile_size": 512, "model_lock_id": l.RuntimeLock, "resource_estimate": map[string]int64{"ram_bytes": 1 << 30, "vram_bytes": map[string]int64{"cpu": 0, "mps": 2 << 30, "cuda": 2 << 30}[l.Backend], "scratch_bytes": source.Bytes + out, "output_bytes": out, "pixel_count": pixels}})
	if err != nil {
		return result, err
	}
	return InteractiveMaskMaterialization{Inputs: unique, Digest: digest, Profile: profile}, nil
}

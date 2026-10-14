package v2input

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
)

// Complete input-only preparation for the existing SFD worker. These entries
// exclude Params, which the caller publishes and adds to the frozen closure.
// Source/catalog/ICC authorization, scientific qualification, GPU resource
// accounting, native producer lineage and locked scheduling remain required.
type SemanticFaceDetectPreparation struct {
	Params json.RawMessage
	Inputs []Entry
}

func PrepareSemanticFaceDetectParams(ctx context.Context, binding SessionVersionBinding, imageBody json.RawMessage, modelLock string, scoreThreshold float64, maxFaces int) (SemanticFaceDetectPreparation, error) {
	var zero SemanticFaceDetectPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(imageBody) < 2 || len(imageBody) > 1<<20 || scoreThreshold < .5 || scoreThreshold > .99 || maxFaces < 1 || maxFaces > 100 {
		return zero, ErrInvalid
	}
	if _, err := domain.ParseDigest(modelLock); err != nil {
		return zero, ErrInvalid
	}
	inputs, err := prepareSemanticFaceSourceInputs(ctx, binding, imageBody)
	if err != nil {
		return zero, err
	}
	params, err := canonical.Marshal(review.FaceDetectParams{Image: review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}, ModelID: "sfd", ModelLockID: modelLock, PreprocessingID: "sfd_long1024_bgr_v1", LongSide: 1024, ScoreThreshold: scoreThreshold, NMSIOU: .3, MaxFaces: maxFaces})
	if err != nil {
		return zero, ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return SemanticFaceDetectPreparation{Params: params, Inputs: inputs}, nil
}

func prepareSemanticFaceSourceInputs(ctx context.Context, binding SessionVersionBinding, imageBody json.RawMessage) ([]Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(imageBody) < 2 || len(imageBody) > 1<<20 {
		return nil, ErrInvalid
	}
	metadata, err := ResolveSemanticSourceMetadata(binding, imageBody)
	if err != nil {
		return nil, err
	}
	inputs := []Entry{{Ref: binding.Source, Contract: "WorkingImage"}, {Ref: metadata.Inputs.Pixels, Contract: "NpyArray"}}
	seen := map[uuid.UUID]Ref{}
	for _, entry := range inputs {
		if !validEntry(entry) || seen[entry.Ref.ArtifactID].ArtifactID != uuid.Nil {
			return nil, ErrInvalid
		}
		seen[entry.Ref.ArtifactID] = entry.Ref
	}
	if metadata.Inputs.Alpha != nil {
		ref := *metadata.Inputs.Alpha
		if !validRef(ref) || seen[ref.ArtifactID].ArtifactID != uuid.Nil {
			return nil, ErrInvalid
		}
		seen[ref.ArtifactID] = ref
		inputs = append(inputs, Entry{Ref: ref, Contract: "NpyArray"})
	}
	geometry := map[uuid.UUID]Ref{}
	for _, ref := range metadata.Geometry {
		if previous, exists := geometry[ref.ArtifactID]; exists {
			if previous != ref {
				return nil, ErrInvalid
			}
			continue
		}
		if !validRef(ref) || seen[ref.ArtifactID].ArtifactID != uuid.Nil {
			return nil, ErrInvalid
		}
		seen[ref.ArtifactID] = ref
		geometry[ref.ArtifactID] = ref
		inputs = append(inputs, Entry{Ref: ref, Contract: "NpyArray"})
	}
	sort.Slice(inputs, func(i, j int) bool {
		return bytes.Compare(inputs[i].Ref.ArtifactID[:], inputs[j].Ref.ArtifactID[:]) < 0
	})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return inputs, nil
}

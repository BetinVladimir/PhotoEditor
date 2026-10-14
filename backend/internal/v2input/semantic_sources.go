package v2input

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

type SemanticSourceMetadata struct {
	Width, Height int64
	Gamma         bool
	Inputs        CaptureImageInputs
	Geometry      []Ref
}

// ResolveSemanticSourceMetadata parses exact pinned bytes. Catalog/project
// authority and ICC qualification must be established before supplying them.
func ResolveSemanticSourceMetadata(binding SessionVersionBinding, body json.RawMessage) (SemanticSourceMetadata, error) {
	var empty SemanticSourceMetadata
	if !semanticBindingValid(binding) || len(body) < 2 || len(body) > 8*1024*1024 {
		return empty, ErrInvalid
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != binding.Source.SHA256 {
		return empty, ErrInvalid
	}
	image, err := review.ParseWorkingImage(body)
	if err != nil || image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" || image.Color.Encoding != "gamma24" {
		return empty, ErrInvalid
	}
	hash, err := canonical.Hash(image.Canvas)
	if err != nil || hash != binding.CanvasHash {
		return empty, ErrInvalid
	}
	width, height := int64(image.Canvas.Width), int64(image.Canvas.Height)
	if width < 8 || height < 8 || width > 200000000 || height > 200000000 || width*height > 200000000 {
		return empty, ErrInvalid
	}
	metadata := SemanticSourceMetadata{Width: width, Height: height, Gamma: true, Inputs: CaptureImageInputs{Pixels: Ref{image.Pixels.ID, image.Pixels.SHA256}}}
	if image.Alpha != nil {
		value := Ref{image.Alpha.ID, image.Alpha.SHA256}
		metadata.Inputs.Alpha = &value
	}
	var chain struct {
		Steps []struct {
			Kind         string `json:"kind"`
			InverseField *Ref   `json:"inverse_field"`
		}
	}
	if json.Unmarshal(image.TransformChain, &chain) != nil || len(chain.Steps) > 32 {
		return empty, ErrInvalid
	}
	for _, step := range chain.Steps {
		if step.Kind == "dense_warp" {
			if step.InverseField == nil || !validRef(*step.InverseField) {
				return empty, ErrInvalid
			}
			metadata.Geometry = append(metadata.Geometry, *step.InverseField)
		}
	}
	return metadata, nil
}

// CompileSemanticTransferTaskFromSources derives all source dimensions,
// encodings, RGB/alpha/geometry children before resource/task compilation.
// Mask/protection dependencies remain independently verified resolver inputs.
func CompileSemanticTransferTaskFromSources(workflow uuid.UUID, ordinal int, params SemanticTransferParams, intent SemanticEditIntent,
	paramsRef Ref, dependencies SemanticTransferDependencies, reference, target json.RawMessage, profile json.RawMessage) (FrozenSemanticTask, error) {
	source, err := ResolveSemanticSourceMetadata(params.Reference, reference)
	if err != nil {
		return FrozenSemanticTask{}, err
	}
	destination, err := ResolveSemanticSourceMetadata(params.Target, target)
	if err != nil {
		return FrozenSemanticTask{}, err
	}
	dependencies.ReferenceWidth, dependencies.ReferenceHeight = source.Width, source.Height
	dependencies.TargetWidth, dependencies.TargetHeight = destination.Width, destination.Height
	dependencies.ReferenceGamma, dependencies.TargetGamma = source.Gamma, destination.Gamma
	dependencies.Reference, dependencies.Target = source.Inputs, destination.Inputs
	dependencies.Geometry = append(append([]Ref(nil), source.Geometry...), destination.Geometry...)
	return CompileSemanticTransferTask(workflow, ordinal, params, intent, paramsRef, dependencies, profile)
}

// SemanticGeometryArray carries the inverse field grid of its own transform
// step. Earlier transform grids may differ from the final WorkingImage canvas.
type SemanticGeometryArray struct {
	Ref           Ref
	Width, Height int64
}

func ResolveSemanticGeometryArrays(binding SessionVersionBinding, body json.RawMessage) ([]SemanticGeometryArray, error) {
	if _, err := ResolveSemanticSourceMetadata(binding, body); err != nil {
		return nil, err
	}
	image, err := review.ParseWorkingImage(body)
	if err != nil {
		return nil, ErrInvalid
	}
	var chain struct {
		Steps []struct {
			Kind    string        `json:"kind"`
			To      review.Canvas `json:"to_canvas"`
			Inverse *Ref          `json:"inverse_field"`
		}
	}
	if json.Unmarshal(image.TransformChain, &chain) != nil {
		return nil, ErrInvalid
	}
	arrays := make([]SemanticGeometryArray, 0)
	for _, step := range chain.Steps {
		if step.Kind != "dense_warp" {
			continue
		}
		width, height := int64(step.To.Width), int64(step.To.Height)
		if step.Inverse == nil || !validRef(*step.Inverse) || width < 1 || height < 1 || width > 200000000 || height > 200000000 || width*height > 200000000 {
			return nil, ErrInvalid
		}
		arrays = append(arrays, SemanticGeometryArray{Ref: *step.Inverse, Width: width, Height: height})
	}
	return arrays, nil
}

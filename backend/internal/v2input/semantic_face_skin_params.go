package v2input

import (
	"bytes"
	"context"
	"encoding/json"
	"sort"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

type SemanticFaceSkinPreparation struct {
	Params json.RawMessage
	Inputs []Entry
}

// PrepareSemanticFaceSkinParams preserves the exact parser label map. It proves
// metadata consistency, not physical uint16 label content, producer authority or
// model qualification; those must be checked before publishing and admission.
func PrepareSemanticFaceSkinParams(ctx context.Context, binding SessionVersionBinding, imageBody json.RawMessage, labels Ref, labelsBody json.RawMessage, parserParams Ref, parserBody json.RawMessage, detection Ref, detectionBody json.RawMessage) (SemanticFaceSkinPreparation, error) {
	var zero SemanticFaceSkinPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	sourceInputs, err := prepareSemanticFaceSourceInputs(ctx, binding, imageBody)
	if err != nil {
		return zero, err
	}
	if !validRef(labels) || !validRef(parserParams) || !validRef(detection) {
		return zero, ErrInvalid
	}
	for _, entry := range sourceInputs {
		if entry.Ref.ArtifactID == labels.ArtifactID || entry.Ref.ArtifactID == parserParams.ArtifactID || entry.Ref.ArtifactID == detection.ArtifactID {
			return zero, ErrInvalid
		}
	}
	image, err := review.ParseWorkingImage(imageBody)
	if err != nil {
		return zero, ErrInvalid
	}
	toReview := func(ref Ref) review.ArtifactRef { return review.ArtifactRef{ID: ref.ArtifactID, SHA256: ref.SHA256} }
	parsed, err := review.VerifyFaceParseParams(parserBody, toReview(parserParams), labelsBody, toReview(labels), detectionBody, toReview(detection), image.Source(toReview(binding.Source)))
	if err != nil {
		return zero, err
	}
	// The uint16 map is a child of the manifest, never an alias of any root,
	// original Params, detector artifact or source child.
	mapRef := Ref{ArtifactID: parsed.Labels.ID, SHA256: parsed.Labels.SHA256}
	if mapRef.ArtifactID == labels.ArtifactID || mapRef.ArtifactID == parserParams.ArtifactID || mapRef.ArtifactID == detection.ArtifactID {
		return zero, ErrInvalid
	}
	for _, entry := range sourceInputs {
		if entry.Ref.ArtifactID == mapRef.ArtifactID {
			return zero, ErrInvalid
		}
	}
	params, err := canonical.Marshal(struct {
		Labels    review.ArtifactRef `json:"labels"`
		Algorithm string             `json:"algorithm"`
	}{toReview(labels), "bisenet_face19_skin_v1"})
	if err != nil {
		return zero, ErrInvalid
	}
	inputs := []Entry{{Ref: labels, Contract: "FaceLabels19"}, {Ref: mapRef, Contract: "NpyArray"}}
	sort.Slice(inputs, func(i, j int) bool {
		return bytes.Compare(inputs[i].Ref.ArtifactID[:], inputs[j].Ref.ArtifactID[:]) < 0
	})
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return SemanticFaceSkinPreparation{Params: params, Inputs: inputs}, nil
}

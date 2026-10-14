package v2input

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// CompileSemanticFaceParseClosure derives every input from the original source
// and verified SFD output/Params. Original detector Params are provenance proof,
// not the BiSeNet task's Params or a substitute for its FaceBoxes input.
// Producer authority, qualification and SQL admission remain separate gates.
func CompileSemanticFaceParseClosure(ctx context.Context, workflow uuid.UUID, ordinal int, binding SessionVersionBinding, imageBody json.RawMessage, faces Ref, facesBody json.RawMessage, detectorParams Ref, detectorBody json.RawMessage, paramsRef Ref, modelLock string) (CaptureClosure, error) {
	var zero CaptureClosure
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !validRef(paramsRef) || paramsRef.ArtifactID == detectorParams.ArtifactID {
		return zero, ErrInvalid
	}
	prepared, err := PrepareSemanticFaceParseParams(ctx, binding, imageBody, faces, facesBody, detectorParams, detectorBody, modelLock)
	if err != nil {
		return zero, err
	}
	for _, entry := range prepared.Inputs {
		if entry.Ref.ArtifactID == paramsRef.ArtifactID {
			return zero, ErrInvalid
		}
	}
	task, err := TaskID(workflow, "face.parse", ordinal)
	if err != nil {
		return zero, err
	}
	entries := append(append([]Entry{}, prepared.Inputs...), Entry{Ref: paramsRef, Contract: "Params"})
	closure, err := compileCaptureInventory(task, "face.parse", prepared.Params, paramsRef, entries)
	if err != nil {
		return zero, err
	}
	metadata, err := ResolveSemanticSourceMetadata(binding, imageBody)
	if err != nil {
		return zero, err
	}
	closure.Direct = []Ref{paramsRef, binding.Source, metadata.Inputs.Pixels}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return closure, nil
}

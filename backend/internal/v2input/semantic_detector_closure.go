package v2input

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// CompileSemanticDetectorClosure revalidates source/Params before freezing the
// inventory for native transport. It never accepts caller-supplied child refs.
// The caller must publish Params and then authorize/register the blocked task
// with qualified GPU profile and producer lineage before queue activation.
func CompileSemanticDetectorClosure(ctx context.Context, workflow uuid.UUID, ordinal int, binding SessionVersionBinding, imageBody json.RawMessage, paramsRef Ref, modelLock string, scoreThreshold float64, maxFaces int) (CaptureClosure, error) {
	if err := ctx.Err(); err != nil {
		return CaptureClosure{}, err
	}
	if !validRef(paramsRef) {
		return CaptureClosure{}, ErrInvalid
	}
	prepared, err := PrepareSemanticFaceDetectParams(ctx, binding, imageBody, modelLock, scoreThreshold, maxFaces)
	if err != nil {
		return CaptureClosure{}, err
	}
	for _, entry := range prepared.Inputs {
		if entry.Ref.ArtifactID == paramsRef.ArtifactID {
			return CaptureClosure{}, ErrInvalid
		}
	}
	task, err := TaskID(workflow, "face.detect", ordinal)
	if err != nil {
		return CaptureClosure{}, err
	}
	entries := append(append([]Entry{}, prepared.Inputs...), Entry{Ref: paramsRef, Contract: "Params"})
	closure, err := compileCaptureInventory(task, "face.detect", prepared.Params, paramsRef, entries)
	if err != nil {
		return CaptureClosure{}, err
	}
	// Input-only stages have bounded source bootstrap; all children remain in the
	// immutable manifest and cursor used by task-input registration.
	metadata, err := ResolveSemanticSourceMetadata(binding, imageBody)
	if err != nil {
		return CaptureClosure{}, err
	}
	closure.Direct = []Ref{paramsRef, binding.Source, metadata.Inputs.Pixels}
	if err := ctx.Err(); err != nil {
		return CaptureClosure{}, err
	}
	return closure, nil
}

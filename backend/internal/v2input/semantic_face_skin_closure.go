package v2input

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
)

// CompileSemanticFaceSkinClosure binds the CPU task to the exact parser manifest
// and uint16 child map. Source and upstream documents are verification evidence;
// they are not downloaded by mask.face_skin. Physical labels, producer authority
// and locked task admission must still be verified separately.
func CompileSemanticFaceSkinClosure(ctx context.Context, workflow uuid.UUID, ordinal int, binding SessionVersionBinding, imageBody json.RawMessage, labels Ref, labelsBody json.RawMessage, parserParams Ref, parserBody json.RawMessage, detection Ref, detectionBody json.RawMessage, paramsRef Ref) (CaptureClosure, error) {
	var zero CaptureClosure
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !validRef(paramsRef) || paramsRef.ArtifactID == binding.Source.ArtifactID || paramsRef.ArtifactID == parserParams.ArtifactID || paramsRef.ArtifactID == detection.ArtifactID {
		return zero, ErrInvalid
	}
	prepared, err := PrepareSemanticFaceSkinParams(ctx, binding, imageBody, labels, labelsBody, parserParams, parserBody, detection, detectionBody)
	if err != nil {
		return zero, err
	}
	// Check source child aliases as well as the two actual worker inputs.
	source, err := prepareSemanticFaceSourceInputs(ctx, binding, imageBody)
	if err != nil {
		return zero, err
	}
	for _, entry := range append(append([]Entry{}, source...), prepared.Inputs...) {
		if entry.Ref.ArtifactID == paramsRef.ArtifactID {
			return zero, ErrInvalid
		}
	}
	task, err := TaskID(workflow, "mask.face_skin", ordinal)
	if err != nil {
		return zero, err
	}
	entries := append(append([]Entry{}, prepared.Inputs...), Entry{Ref: paramsRef, Contract: "Params"})
	closure, err := compileCaptureInventory(task, "mask.face_skin", prepared.Params, paramsRef, entries)
	if err != nil {
		return zero, err
	}
	var mapRef Ref
	for _, entry := range prepared.Inputs {
		if entry.Contract == "NpyArray" {
			mapRef = entry.Ref
		}
	}
	closure.Direct = []Ref{paramsRef, labels, mapRef}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return closure, nil
}

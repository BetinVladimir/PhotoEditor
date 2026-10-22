package v2render

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// AuthoredFacePrerequisiteIDs gives each real inference stage an independent
// instruction-owned run. Results are never treated as a selection decision.
func AuthoredFacePrerequisiteIDs(source AuthoredInstructionSource, operation string) (uuid.UUID, uuid.UUID, error) {
	if source.Tenant == uuid.Nil || source.Parent == uuid.Nil || source.Workflow != uuid.NewSHA1(source.Parent, []byte("workflow")) || source.Instruction.Kind != "segment" || len(source.Instruction.Layers) != 1 || source.Instruction.GeometryLayer != nil || !authoredBindingValid(source.Source) {
		return uuid.Nil, uuid.Nil, v2input.ErrInvalid
	}
	instruction, err := v2input.TaskID(source.Workflow, "render.authored_segment", source.Instruction.Ordinal)
	if err != nil || instruction != source.Instruction.Identity {
		return uuid.Nil, uuid.Nil, v2input.ErrInvalid
	}
	switch operation {
	case "face.detect", "face.landmarks", "face.parse":
	default:
		return uuid.Nil, uuid.Nil, v2input.ErrInvalid
	}
	run := uuid.NewSHA1(instruction, []byte("AuthoredRepairFace/v1:"+operation))
	workflow, err := v2input.WorkflowID(run)
	return run, workflow, err
}

type AuthoredFaceBudget struct {
	Backend                    string
	Sizes                      []v2input.SemanticInputSize
	Costs                      v2input.SemanticQualifiedCosts
	RAM, VRAM, Scratch, Output int64
}

// These ports reuse the actual model Params/closure/profile compilers. Qualified
// source/producer publication and SQL admission remain mandatory. A caller cannot
// replace the detector with copied authored landmarks or a geometric rewrite.
func CompileAuthoredFaceDetector(ctx context.Context, source AuthoredInstructionSource, image json.RawMessage, params v2input.Ref, model string, score float64, maxFaces int, budget AuthoredFaceBudget) (v2input.FrozenSemanticDetectorTask, error) {
	_, workflow, err := AuthoredFacePrerequisiteIDs(source, "face.detect")
	if err != nil {
		return v2input.FrozenSemanticDetectorTask{}, err
	}
	return v2input.CompileSemanticDetectorTaskForBackend(budget.Backend, ctx, workflow, 0, source.Source, image, params, model, score, maxFaces, budget.Sizes, budget.Costs, budget.RAM, budget.VRAM, budget.Scratch, budget.Output)
}
func CompileAuthoredFaceLandmarks(ctx context.Context, source AuthoredInstructionSource, image json.RawMessage, faces v2input.Ref, faceBody json.RawMessage, detectorParams v2input.Ref, detectorBody json.RawMessage, params v2input.Ref, model string, budget AuthoredFaceBudget) (v2input.FrozenSemanticLandmarkTask, error) {
	_, workflow, err := AuthoredFacePrerequisiteIDs(source, "face.landmarks")
	if err != nil {
		return v2input.FrozenSemanticLandmarkTask{}, err
	}
	return v2input.CompileSemanticLandmarkTaskForBackend(budget.Backend, ctx, workflow, 0, source.Source, image, faces, faceBody, detectorParams, detectorBody, params, model, budget.Sizes, budget.Costs, budget.RAM, budget.VRAM, budget.Scratch, budget.Output)
}
func CompileAuthoredFaceParser(ctx context.Context, source AuthoredInstructionSource, image json.RawMessage, faces v2input.Ref, faceBody json.RawMessage, detectorParams v2input.Ref, detectorBody json.RawMessage, params v2input.Ref, model string, budget AuthoredFaceBudget) (v2input.FrozenSemanticFaceParseTask, error) {
	_, workflow, err := AuthoredFacePrerequisiteIDs(source, "face.parse")
	if err != nil {
		return v2input.FrozenSemanticFaceParseTask{}, err
	}
	return v2input.CompileSemanticFaceParseTaskForBackend(budget.Backend, ctx, workflow, 0, source.Source, image, faces, faceBody, detectorParams, detectorBody, params, model, budget.Sizes, budget.Costs, budget.RAM, budget.VRAM, budget.Scratch, budget.Output)
}

// CompileAuthoredFaceSkin retains the installed CPU mask.face_skin Params compiler.
// The detector/parser are actual current-source outputs; selected objects are
// applied later from persisted review, never inferred from prior face IDs.
func CompileAuthoredFaceSkin(ctx context.Context, source AuthoredInstructionSource, image json.RawMessage, labels v2input.Ref, labelsBody json.RawMessage, parserParams v2input.Ref, parserBody json.RawMessage, detection v2input.Ref, detectionBody json.RawMessage, params v2input.Ref, budget AuthoredFaceBudget) (v2input.FrozenSemanticFaceSkinTask, error) {
	if budget.Backend != "cpu" || budget.VRAM != 0 {
		return v2input.FrozenSemanticFaceSkinTask{}, v2input.ErrInvalid
	}
	// Reuse source/instruction validation without pretending the CPU mask is a
	// fan2d/parser model inference or reusing their qualification label.
	_, _, err := AuthoredFacePrerequisiteIDs(source, "face.parse")
	if err != nil {
		return v2input.FrozenSemanticFaceSkinTask{}, err
	}
	run := uuid.NewSHA1(source.Instruction.Identity, []byte("AuthoredRepairFace/v1:mask.face_skin"))
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return v2input.FrozenSemanticFaceSkinTask{}, err
	}
	return v2input.CompileSemanticFaceSkinTask(ctx, workflow, 0, source.Source, image, labels, labelsBody, parserParams, parserBody, detection, detectionBody, params, budget.Sizes, budget.RAM, budget.Scratch, budget.Output)
}

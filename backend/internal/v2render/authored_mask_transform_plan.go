package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Original declarations stay immutable across a bounded sequence of real
// adopted transform children. Neither descriptor nor compiler is SQL authority.
type AuthoredMaskTransformPrevious struct {
	Task    uuid.UUID   `json:"task_id"`
	Result  uuid.UUID   `json:"result_id"`
	Attempt int         `json:"attempt"`
	Fence   int64       `json:"fence"`
	Root    v2input.Ref `json:"root"`
}
type AuthoredMaskTransformDescriptor struct {
	SchemaVersion        int                            `json:"schema_version"`
	Parent               uuid.UUID                      `json:"parent_run_id"`
	Instruction          uuid.UUID                      `json:"instruction_id"`
	ProgramSHA           string                         `json:"program_sha256"`
	PredecessorSHA       string                         `json:"predecessor_sha256"`
	OriginalMask         review.MaskRef                 `json:"original_mask"`
	OriginalParams       v2input.Ref                    `json:"original_params"`
	OriginalParamsJCS    string                         `json:"original_params_jcs"`
	OriginalRepairJCS    *string                        `json:"original_repair_jcs,omitempty"`
	OriginalProtectedRef *v2input.Ref                   `json:"original_protected_ref,omitempty"`
	OriginalProtectedJCS *string                        `json:"original_protected_jcs,omitempty"`
	OriginalLayerMaskRef *v2input.Ref                   `json:"original_layer_mask_ref,omitempty"`
	OriginalLayerMaskJCS *string                        `json:"original_layer_mask_jcs,omitempty"`
	SourceMask           review.MaskRef                 `json:"source_mask"`
	Target               v2input.Ref                    `json:"target_image"`
	GeometryReceipt      v2input.Ref                    `json:"geometry_phase_receipt"`
	GeometryOrdinal      int                            `json:"geometry_ordinal"`
	GeometryTask         uuid.UUID                      `json:"geometry_task_id"`
	GeometryResult       uuid.UUID                      `json:"geometry_result_id"`
	GeometryAttempt      int                            `json:"geometry_attempt"`
	GeometryFence        int64                          `json:"geometry_fence"`
	Previous             *AuthoredMaskTransformPrevious `json:"previous_transform,omitempty"`
}
type AuthoredMaskTransformPlan struct {
	Run, Workflow, Task uuid.UUID
	Descriptor          AuthoredMaskTransformDescriptor
	DescriptorJCS       string
	Params              v2input.Ref
	ParamsJCS           string
	Frozen              v2input.FrozenAuthoredMaskTransformTask
}

func AuthoredMaskTransformIDs(d AuthoredMaskTransformDescriptor) (uuid.UUID, uuid.UUID, uuid.UUID, error) {
	if d.SchemaVersion != 1 || d.Instruction == uuid.Nil || d.Parent == uuid.Nil || !validLayerArtifactRef(d.Target) {
		return uuid.Nil, uuid.Nil, uuid.Nil, v2input.ErrInvalid
	}
	sourceSHA, err := canonical.Hash(d.SourceMask)
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, err
	}
	authoritySHA, err := canonical.Hash(struct {
		OriginalMask         review.MaskRef `json:"original_mask"`
		OriginalParams       v2input.Ref    `json:"original_params"`
		OriginalRepairJCS    *string        `json:"original_repair_jcs"`
		OriginalProtectedRef *v2input.Ref   `json:"original_protected_ref"`
		OriginalLayerMaskRef *v2input.Ref   `json:"original_layer_mask_ref"`
	}{d.OriginalMask, d.OriginalParams, d.OriginalRepairJCS, d.OriginalProtectedRef, d.OriginalLayerMaskRef})
	if err != nil {
		return uuid.Nil, uuid.Nil, uuid.Nil, err
	}
	run := uuid.NewSHA1(d.Instruction, []byte("AuthoredMaskTransform/v2:"+authoritySHA+":"+sourceSHA+":"+d.Target.ArtifactID.String()+":"+d.Target.SHA256))
	workflow := uuid.NewSHA1(run, []byte("workflow"))
	task, err := v2input.TaskID(workflow, "mask.transform", 0)
	return run, workflow, task, err
}

func CompileAuthoredMaskTransformPlan(ctx context.Context, source AuthoredInstructionSource, d AuthoredMaskTransformDescriptor, targetRaw []byte, sizes []v2input.SemanticInputSize, ram, scratch, output int64) (AuthoredMaskTransformPlan, error) {
	var zero AuthoredMaskTransformPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if source.Instruction.Kind != "segment" || len(source.Instruction.Layers) != 1 || source.Instruction.GeometryLayer != nil || d.Parent != source.Parent || d.Instruction != source.Instruction.Identity || d.ProgramSHA != source.ProgramSHA || d.PredecessorSHA != source.PredecessorSHA || d.GeometryOrdinal < 1 || d.GeometryOrdinal >= source.Instruction.Ordinal || d.GeometryTask == uuid.Nil || d.GeometryResult == uuid.Nil || d.GeometryAttempt < 1 || d.GeometryFence < 1 || !validLayerArtifactRef(d.GeometryReceipt) || !validLayerArtifactRef(d.OriginalParams) {
		return zero, v2input.ErrInvalid
	}
	for _, digest := range []string{d.ProgramSHA, d.PredecessorSHA} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return zero, v2input.ErrInvalid
		}
	}
	paramsCanonical, err := canonical.Bytes([]byte(d.OriginalParamsJCS))
	if err != nil || !bytes.Equal(paramsCanonical, []byte(d.OriginalParamsJCS)) {
		return zero, v2input.ErrInvalid
	}
	paramsHash, err := canonical.Hash(json.RawMessage(d.OriginalParamsJCS))
	if err != nil || paramsHash != d.OriginalParams.SHA256 {
		return zero, v2input.ErrInvalid
	}
	if err := validateAuthoredMaskOriginalWitness(source, d); err != nil {
		return zero, err
	}
	originalRaw, err := canonical.Marshal(d.OriginalMask)
	if err != nil {
		return zero, err
	}
	if _, err = review.ParseMaskRef(originalRaw); err != nil {
		return zero, err
	}
	var a, b struct {
		Steps []json.RawMessage `json:"steps"`
		Hash  string            `json:"hash"`
	}
	if json.Unmarshal(d.OriginalMask.TransformChain, &a) != nil || json.Unmarshal(d.SourceMask.TransformChain, &b) != nil || len(b.Steps) < len(a.Steps) || len(b.Steps)-len(a.Steps) >= 128 {
		return zero, v2input.ErrInvalid
	}
	if !authoredMaskChainPrefix(a.Steps, b.Steps) {
		return zero, v2input.ErrInvalid
	}
	originalMeta, sourceMeta := d.OriginalMask, d.SourceMask
	originalMeta.Artifact = sourceMeta.Artifact
	originalMeta.Canvas = sourceMeta.Canvas
	originalMeta.TransformChain = sourceMeta.TransformChain
	expected, err := canonical.Marshal(originalMeta)
	if err != nil {
		return zero, err
	}
	actual, err := canonical.Marshal(sourceMeta)
	if err != nil || string(expected) != string(actual) {
		return zero, v2input.ErrInvalid
	}
	if d.Previous == nil {
		left, _ := canonical.Marshal(d.OriginalMask)
		right, _ := canonical.Marshal(d.SourceMask)
		if string(left) != string(right) {
			return zero, v2input.ErrInvalid
		}
	} else if d.Previous.Task == uuid.Nil || d.Previous.Result == uuid.Nil || d.Previous.Attempt < 1 || d.Previous.Fence < 1 || !validLayerArtifactRef(d.Previous.Root) {
		return zero, v2input.ErrInvalid
	}
	// Own every nested mask/raw/pointer before publication or caller IO.
	wire, err := canonical.Marshal(d)
	if err != nil {
		return zero, err
	}
	var owned AuthoredMaskTransformDescriptor
	if json.Unmarshal(wire, &owned) != nil {
		return zero, v2input.ErrInvalid
	}
	d = owned
	run, workflow, task, err := AuthoredMaskTransformIDs(d)
	if err != nil {
		return zero, err
	}
	maskRaw, err := canonical.Marshal(d.SourceMask)
	if err != nil {
		return zero, err
	}
	native, entries, err := v2input.PrepareAuthoredMaskTransformParams(ctx, maskRaw, targetRaw, d.Target)
	if err != nil {
		return zero, err
	}
	raw, err := canonical.Marshal(native)
	if err != nil {
		return zero, err
	}
	digest, err := canonical.Hash(native)
	if err != nil {
		return zero, err
	}
	params := v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte("params")), SHA256: digest}
	entries = append(entries, v2input.Entry{Ref: params, Contract: "Params"})
	frozen, err := v2input.CompileAuthoredMaskTransformTask(ctx, workflow, maskRaw, targetRaw, d.Target, params, entries, sizes, ram, scratch, output)
	if err != nil {
		return zero, err
	}
	return AuthoredMaskTransformPlan{Run: run, Workflow: workflow, Task: task, Descriptor: d, DescriptorJCS: string(wire), Params: params, ParamsJCS: string(raw), Frozen: frozen}, ctx.Err()
}

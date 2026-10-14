package provenance

import (
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// CompileSigningRenderChildOutputs only accepts invocation-local physical
// evidence tied to the exact canonical task. It grants neither an action nor
// complete ancestry; model/review and causal inputs remain separate checks.
func CompileSigningRenderChildOutputs(task CanonicalSigningTask, evidence SigningRenderEvidence) ([]SigningChildOutput, error) {
	if normalizeCanonicalSigningTask(&task) != nil || task.Operation != "render.compose" || task.Version != "1" {
		return nil, ErrInvalid
	}
	digest, err := canonical.Hash(task)
	if err != nil || digest != evidence.taskDigest {
		return nil, ErrInvalid
	}
	outputs, err := canonicalTaskOutputRefs(task.Outputs)
	if err != nil || len(outputs) != 1 || outputs[0] != evidence.pixels.root {
		return nil, ErrInvalid
	}
	result := evidence.pixels.result
	if result.SchemaVersion != 1 {
		return nil, ErrInvalid
	}
	children := []SigningChildOutput{{Root: outputs[0], Ref: result.WorkingImage, Role: "working_image"}, {Root: outputs[0], Ref: result.ChangedMask, Role: "changed_mask"}, {Root: outputs[0], Ref: result.Report, Role: "report"}}
	if result.GeneratedMask != nil {
		children = append(children, SigningChildOutput{Root: outputs[0], Ref: *result.GeneratedMask, Role: "generated_mask"})
	}
	node := SigningExecutionNode{Outputs: append([]v2input.Ref(nil), outputs...), Children: children, Action: SigningAction{Operation: task.Operation, Version: task.Version}}
	if _, err := signingNodeContentOutputs(node); err != nil {
		return nil, err
	}
	return children, nil
}

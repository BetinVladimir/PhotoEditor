package v2render

import (
	"bytes"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Policy describes fixed orchestration behavior, not model/runtime/device or
// measured resource qualification. Changes require a new supported version.
type RenderWorkflowPolicy struct {
	SchemaVersion      int    `json:"schema_version"`
	RendererVersion    string `json:"renderer_version"`
	WorkflowType       string `json:"workflow_type"`
	ChildFailurePolicy string `json:"child_failure_policy"`
	MaxFrames          int    `json:"max_frames"`
}
type RenderPolicySnapshot struct {
	PolicySHA string               `json:"policy_sha"`
	Policy    RenderWorkflowPolicy `json:"policy"`
}

func supportedRenderPolicy() RenderWorkflowPolicy {
	return RenderWorkflowPolicy{1, "v2.2", "preview", "fail_fast", 5000}
}

// DefaultRenderPolicy reflects CompileRenderBatchPlans/CompileRenderRunPlan and
// begin_v26_render_draft's existing fixed limits and child workflow behavior.
func DefaultRenderPolicy() (RenderPolicySnapshot, error) {
	policy := supportedRenderPolicy()
	digest, err := canonical.Hash(policy)
	return RenderPolicySnapshot{PolicySHA: digest, Policy: policy}, err
}
func DecodeRenderPolicySnapshot(raw []byte) (RenderPolicySnapshot, error) {
	var value RenderPolicySnapshot
	fail := func() (RenderPolicySnapshot, error) { return RenderPolicySnapshot{}, v2input.ErrInvalid }
	if len(raw) == 0 || len(raw) > 4096 {
		return fail()
	}
	body, err := canonical.Bytes(raw)
	if err != nil || json.Unmarshal(body, &value) != nil {
		return fail()
	}
	encoded, err := canonical.Marshal(value)
	if err != nil || !bytes.Equal(encoded, body) || value.Policy != supportedRenderPolicy() {
		return fail()
	}
	digest, err := canonical.Hash(value.Policy)
	if err != nil || digest != value.PolicySHA {
		return fail()
	}
	return value, nil
}

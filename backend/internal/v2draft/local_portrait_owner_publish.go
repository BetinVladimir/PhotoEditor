package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// PublishLocalPortraitOwnerParams uses the committed saved-intent SQL authority.
// Reserve/Complete must both recheck live scope; this caller grants no authority.
func PublishLocalPortraitOwnerParams(ctx context.Context, publisher v2input.DocumentPublisher,
	requestRaw, sourceRaw []byte, runtimeSHA string, capacity v2portrait.LocalResourceEstimate) (v2portrait.PublishedLocalTaskPlan, error) {
	var zero v2portrait.PublishedLocalTaskPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if len(requestRaw) < 2 || len(requestRaw) > 1048576 {
		return zero, v2input.ErrInvalid
	}
	var request LocalPortraitOwnerRequest
	if json.Unmarshal(requestRaw, &request) != nil || request.SchemaVersion != 1 {
		return zero, v2input.ErrInvalid
	}
	expected, err := canonical.Marshal(request)
	if err != nil || !bytes.Equal(expected, requestRaw) {
		return zero, v2input.ErrInvalid
	}
	workflow, err := v2input.WorkflowID(request.Run)
	if err != nil || workflow != request.Workflow {
		return zero, v2input.ErrInvalid
	}
	task, err := v2input.TaskID(workflow, request.Intent.Operation, request.Ordinal)
	if err != nil || task != request.Intent.TaskID {
		return zero, v2input.ErrInvalid
	}
	plan, err := v2portrait.CompileLocalTaskPlan(ctx, request.Intent.Operation, request.Intent.Binding, sourceRaw, []byte(request.Intent.ParamsJCS), runtimeSHA, capacity)
	if err != nil {
		return zero, err
	}
	if plan.ParamsSHA != request.Intent.ParamsSHA || !bytes.Equal(plan.Profile, request.Intent.Profile) || plan.ReservedBytes != request.Intent.ReservedBytes {
		return zero, v2input.ErrInvalid
	}
	return v2portrait.CompileAndPublishLocalTaskPlan(ctx, publisher, artifactjson.Scope{Tenant: request.Tenant, Run: request.Run, Owner: task, Purpose: "task_params", LocalPortrait: true},
		plan.Operation, request.Intent.Binding, sourceRaw, plan.Params, runtimeSHA, capacity)
}

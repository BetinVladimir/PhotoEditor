package v2portrait

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2input"
)

type PublishedLocalTaskPlan struct {
	ParamsRef v2input.Ref
	Plan      LocalTaskPlan
}

// CompileAndPublishLocalTaskPlan completes pure validation before publication.
// The publisher must enforce authorized graph ownership and assembly fencing.
// This does not create graph ownership or seal/register task inputs.
func CompileAndPublishLocalTaskPlan(ctx context.Context, publisher v2input.DocumentPublisher,
	scope artifactjson.Scope, operation string, binding v2input.SessionVersionBinding,
	sourceRaw, paramsRaw []byte, runtimeSHA string, capacity LocalResourceEstimate) (PublishedLocalTaskPlan, error) {
	var zero PublishedLocalTaskPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if publisher == nil || scope.Purpose != "task_params" || scope.Tenant == uuid.Nil || scope.Run == uuid.Nil || scope.Owner == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	plan, err := CompileLocalTaskPlan(ctx, operation, binding, sourceRaw, paramsRaw, runtimeSHA, capacity)
	if err != nil {
		return zero, err
	}
	var header struct {
		Mask      v2input.Ref `json:"mask"`
		Protected v2input.Ref `json:"protected"`
	}
	if json.Unmarshal(plan.Params, &header) != nil {
		return zero, v2input.ErrInvalid
	}
	ref, err := publisher.Publish(ctx, scope, append([]byte(nil), plan.Params...))
	if err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	if ref.ID == uuid.Nil || ref.SHA256 != plan.ParamsSHA || ref.ID == binding.Source.ArtifactID || ref.ID == header.Mask.ArtifactID || ref.ID == header.Protected.ArtifactID {
		return zero, v2input.ErrInvalid
	}
	return PublishedLocalTaskPlan{ParamsRef: v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, Plan: plan}, nil
}

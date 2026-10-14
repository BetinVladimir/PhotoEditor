package v2portrait

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2input"
)

type PublishedDonorTaskPlan struct {
	ParamsRef v2input.Ref
	Plan      DonorTaskPlan
}

// CompileAndPublishDonorTaskPlan requires an existing fenced assembly owner.
// It does not establish donor consent, graph ownership, qualification or sealing.
// The local hair/clothing saved-intent route is deliberately not reused.
func CompileAndPublishDonorTaskPlan(ctx context.Context, publisher v2input.DocumentPublisher,
	scope artifactjson.Scope, base, donor v2input.SessionVersionBinding,
	sourceRaw, donorRaw, paramsRaw []byte, pw, ph int64, runtimeSHA string,
	capacity DonorResourceEstimate) (PublishedDonorTaskPlan, error) {
	var zero PublishedDonorTaskPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if publisher == nil || scope.Purpose != "task_params" || scope.Tenant == uuid.Nil ||
		scope.Run == uuid.Nil || scope.Owner == uuid.Nil || scope.AssemblyLease == uuid.Nil || scope.LocalPortrait {
		return zero, v2input.ErrInvalid
	}
	plan, err := CompileDonorTaskPlan(ctx, base, donor, sourceRaw, donorRaw, paramsRaw, pw, ph, runtimeSHA, capacity)
	if err != nil {
		return zero, err
	}
	ref, err := publisher.Publish(ctx, scope, append([]byte(nil), plan.Params...))
	if err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	if ref.ID == uuid.Nil || ref.SHA256 != plan.ParamsSHA {
		return zero, v2input.ErrInvalid
	}
	for _, input := range plan.Inputs {
		if ref.ID == input.ArtifactID {
			return zero, v2input.ErrInvalid
		}
	}
	for _, input := range plan.PhysicalImageInputs {
		if ref.ID == input.Ref.ArtifactID {
			return zero, v2input.ErrInvalid
		}
	}
	return PublishedDonorTaskPlan{ParamsRef: v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, Plan: plan}, nil
}

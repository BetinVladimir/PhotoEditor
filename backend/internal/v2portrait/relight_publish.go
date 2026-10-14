package v2portrait

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"sort"
)

type PublishedRelightTaskPlan struct {
	ParamsRef v2input.Ref
	Plan      LocalTaskPlan
	Inputs    []v2input.Entry
}

type RelightQualificationEvidence struct {
	Report, Calibration []byte
}

// CompileAndPublishRelightTaskPlan requires existing fenced assembly authority.
// Saved hair/clothing and donor intents cannot authorize relight publication.
// This does not register inputs, establish qualification or activate a graph.
func CompileAndPublishRelightTaskPlan(ctx context.Context, publisher v2input.DocumentPublisher,
	scope artifactjson.Scope, binding v2input.SessionVersionBinding,
	documents RelightInputDocuments, paramsRaw []byte, runtimeSHA string, capacity LocalResourceEstimate,
	pin RelightQualificationPin, evidence RelightQualificationEvidence,
	expected v2qualification.Expected) (PublishedRelightTaskPlan, error) {
	var zero PublishedRelightTaskPlan
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if publisher == nil || scope.Purpose != "task_params" || scope.Tenant == uuid.Nil || scope.Run == uuid.Nil ||
		scope.Owner == uuid.Nil || scope.AssemblyLease == uuid.Nil || scope.LocalPortrait || scope.DonorPortrait {
		return zero, v2input.ErrInvalid
	}
	compiled, err := CompileRelightInputPlan(ctx, binding, documents, paramsRaw, runtimeSHA, capacity, pin)
	if err != nil {
		return zero, err
	}
	plan := compiled.Plan
	var params RelightParams
	if json.Unmarshal(plan.Params, &params) != nil {
		return zero, v2input.ErrInvalid
	}
	if err = VerifyRelightQualificationEvidence(ctx, params, pin, evidence.Report, evidence.Calibration, expected); err != nil {
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
	for _, input := range compiled.Inputs {
		if ref.ID == input.Ref.ArtifactID {
			return zero, v2input.ErrInvalid
		}
	}
	inputs := append([]v2input.Entry(nil), compiled.Inputs...)
	inputs = append(inputs, v2input.Entry{Ref: v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, Contract: "Params"})
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].Ref.ArtifactID.String() < inputs[j].Ref.ArtifactID.String() })
	return PublishedRelightTaskPlan{ParamsRef: v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, Plan: plan, Inputs: inputs}, nil
}

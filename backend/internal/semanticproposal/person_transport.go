package semanticproposal

import (
	"context"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Publish only after the blocked child draft commits. No database transaction
// may span this object IO. Final registration rechecks all plan authority.
func publishPersonDraftTransport(ctx context.Context, plan personDraftPlan, publisher v2input.Publisher) (v2input.PreparedSemanticTask, defectRegistrationProof, error) {
	var task v2input.PreparedSemanticTask
	var proof defectRegistrationProof
	if err := ctx.Err(); err != nil {
		return task, proof, err
	}
	if err := verifyPersonPlanSeal(ctx, plan); err != nil {
		return task, proof, err
	}
	if publisher == nil || len(plan.shell.Tasks) != 1 || len(plan.shell.Edges) != 0 {
		return task, proof, v2input.ErrInvalid
	}
	run, workflow, err := personPreparationIDs(plan.request.RunID)
	if err != nil || plan.shell.Run != run || plan.shell.WorkflowID != workflow || plan.prepared.parent != plan.request.RunID {
		return task, proof, v2input.ErrInvalid
	}
	if err := verifyPersonStageIdentity(plan.request.RunID, plan.prepared); err != nil {
		return task, proof, err
	}
	if err := verifyPersonTaskQualificationBinding(plan.prepared.catalog.frozen, plan.prepared.qualification); err != nil {
		return task, proof, err
	}
	draft, err := plan.prepared.catalog.frozen.DraftTask()
	if err != nil {
		return task, proof, err
	}
	a, err := canonical.Marshal(draft)
	if err != nil {
		return task, proof, err
	}
	b, err := canonical.Marshal(plan.shell.Tasks[0])
	if err != nil || string(a) != string(b) {
		return task, proof, v2input.ErrConflict
	}
	published, err := v2input.PublishSemanticPersonTask(ctx, plan.prepared.catalog.frozen, publisher)
	if err != nil {
		return task, proof, err
	}
	retained, err := freezeDefectRegistrationProof(ctx, published.Graph)
	if err != nil {
		return task, proof, err
	}
	return published, retained, nil
}

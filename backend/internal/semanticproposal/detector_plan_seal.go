package semanticproposal

import (
	"context"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Private compiler fingerprint owns the complete preparatory plan across object
// IO. It does not replace current database authorization or transaction locks.
func detectorPlanDigest(plan detectorDraftPlan) (string, error) {
	return canonical.Hash(map[string]any{
		"request": plan.request, "request_key": plan.requestKey, "published": plan.published,
		"shell": plan.shell, "key": plan.key, "snapshot_ref": plan.anchor.snapshotRef, "snapshot_body": plan.anchor.snapshotBody,
		"policy": plan.policy.body, "input_objects": plan.prepared.inputs, "source_objects": plan.prepared.source.pins,
		"qualified_costs": plan.prepared.costs, "parent": plan.prepared.parent, "params_document": plan.prepared.document,
		"expected": plan.prepared.qualification.expected, "report_ref": plan.prepared.qualification.report.ref,
		"report_object": plan.prepared.qualification.report.object, "metrics": qualificationResourceSeal(plan.prepared.qualification.metrics),
		"task_sha":            plan.prepared.qualification.taskSHA,
		"source_scope":        map[string]any{"tenant": plan.prepared.source.tenant, "project": plan.prepared.source.project, "actor": plan.prepared.source.actor, "session": plan.prepared.source.session, "bucket": plan.prepared.source.bucket, "binding": plan.prepared.source.binding, "icc_sha": plan.prepared.source.iccSHA, "unit_rgb": plan.prepared.source.unitRGB},
		"qualification_scope": map[string]any{"tenant": plan.prepared.qualification.tenant, "project": plan.prepared.qualification.project, "actor": plan.prepared.qualification.actor, "session": plan.prepared.qualification.session, "bucket": plan.prepared.qualification.bucket, "contract": plan.prepared.qualification.report.contract, "backend": plan.prepared.qualification.report.backend},
	})
}
func verifyDetectorPlanSeal(ctx context.Context, plan detectorDraftPlan) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if plan.seal == "" {
		return v2input.ErrInvalid
	}
	digest, err := detectorPlanDigest(plan)
	if err != nil {
		return err
	}
	if digest != plan.seal {
		return v2input.ErrConflict
	}
	return nil
}

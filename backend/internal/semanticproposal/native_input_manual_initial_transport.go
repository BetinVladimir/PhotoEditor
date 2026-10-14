package semanticproposal

import (
	"context"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Invoke only after blocked draft commit. No transaction spans object IO;
// original accepted Params/manual/native/CPU evidence is reread on both sides.
func (s SQLTransferSourceReader) publishNativeInputManualInitialTransport(ctx context.Context, who auth.Identity, plan nativeInputManualInitialDraftPlan, color TransferColorProfile, publisher v2input.Publisher) (v2input.PreparedSemanticTask, defectRegistrationProof, error) {
	var zero v2input.PreparedSemanticTask
	var noProof defectRegistrationProof
	if err := verifyNativeInputManualInitialDraftSeal(ctx, plan); err != nil {
		return zero, noProof, err
	}
	if publisher == nil {
		return zero, noProof, v2input.ErrInvalid
	}
	owned, err := s.compileNativeInputManualInitialDraft(ctx, who, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return zero, noProof, err
	}
	if owned.seal != plan.seal || !reflect.DeepEqual(owned.shell, plan.shell) {
		return zero, noProof, v2input.ErrConflict
	}
	recheck := func() error {
		stage := owned.prepared.stage
		q := stage.qualification.qualification
		current, err := s.prepareNativeInputManualInitialQualification(ctx, who, stage.dependencies, color, q.report.report.ref, q.costs)
		if err != nil {
			return err
		}
		if !sameNativeInputManualInitialStage(ctx, current, stage) {
			return v2input.ErrConflict
		}
		return ctx.Err()
	}
	if err := recheck(); err != nil {
		return zero, noProof, err
	}
	prepared, err := v2input.PublishSemanticDefectTask(ctx, owned.prepared.compiled.frozen, publisher)
	if err != nil {
		return zero, noProof, err
	}
	proof, err := freezeDefectRegistrationProof(ctx, prepared.Graph)
	if err != nil {
		return zero, noProof, err
	}
	if _, err := owned.prepared.compiled.frozen.BindPublishedTransport(ctx, prepared.Graph.Built); err != nil {
		return zero, noProof, err
	}
	task, err := owned.prepared.compiled.frozen.DraftTask()
	if err != nil {
		return zero, noProof, err
	}
	if prepared.Graph.ID != task.ID || prepared.Params.ArtifactID != task.ParamsArtifactID || prepared.Params.SHA256 != task.ParamsSHA || prepared.InputDigest != task.InputDigest {
		return zero, noProof, v2input.ErrConflict
	}
	if err := recheck(); err != nil {
		return zero, noProof, err
	}
	return prepared, proof, nil
}

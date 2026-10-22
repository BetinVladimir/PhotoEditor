package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// The caller owns a scheduler transaction and must roll back every failure.
// Only a blocked draft is persisted. No object IO, registration or queueing
// occurs here; final admission must repeat all these authority barriers.
func (s SQLTransferSourceReader) beginNativeFaceDraftInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, plan nativeFaceDraftPlan, color TransferColorProfile, completed completedNativeDetector) (DraftAdmission, error) {
	var zero DraftAdmission
	if err := verifyNativeFacePlanSeal(ctx, plan); err != nil {
		return zero, err
	}
	if tx == nil {
		return zero, v2input.ErrInvalid
	}
	rebuilt, err := s.compileNativeFaceDraftPlan(ctx, who, plan.detector, plan.prepared, plan.shell.Bytes)
	if err != nil {
		return zero, err
	}
	if rebuilt.seal != plan.seal || completed.catalog != rebuilt.prepared.detector {
		return zero, v2input.ErrConflict
	}
	paramsPin, reportPin, err := marshalNativeFaceDraftPins(ctx, rebuilt)
	if err != nil {
		return zero, err
	}
	parent := rebuilt.detector
	if err := LockInventoryInTransaction(ctx, tx, who, parent.request, parent.published.Inventory); err != nil {
		return zero, err
	}
	if err := LockPublishedIntentInTransaction(ctx, tx, who, parent.request, parent.published); err != nil {
		return zero, err
	}
	if err := LockSnapshotInTransaction(ctx, tx, who, parent.request, parent.anchor); err != nil {
		return zero, err
	}
	selected, err := selectNativeQualificationPolicies(parent.policy, []string{"face.detect"})
	if err != nil {
		return zero, err
	}
	if err := s.lockSourceDetectorPreparation(ctx, tx, who, parent.request.TenantID, parent.request.Intent.Reference, color, parent.prepared, selected["face.detect"]); err != nil {
		return zero, err
	}
	if err := s.lockCompletedDetectorInTransaction(ctx, tx, who, parent, completed); err != nil {
		return zero, err
	}
	query := "SELECT photo.lock_semantic_proposal_face_qualification($1,$2,$3,$4,$5,$6)"
	if rebuilt.prepared.operation == "face.parse" {
		query = "SELECT photo.lock_semantic_proposal_face_parse_qualification($1,$2,$3,$4,$5,$6)"
	}
	var ready bool
	if err := tx.QueryRow(ctx, query, parent.request.TenantID, s.ProjectID, who.UserID, who.SessionID, rebuilt.prepared.expected, string(reportPin)).Scan(&ready); err != nil {
		return zero, err
	}
	if !ready {
		return zero, v2input.ErrConflict
	}
	binding := parent.request.Intent.Reference
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_version_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", parent.request.TenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, rebuilt.prepared.document, rebuilt.prepared.params.ArtifactID, rebuilt.prepared.params.SHA256, string(paramsPin)).Scan(&ready); err != nil {
		return zero, err
	}
	if !ready {
		return zero, v2input.ErrConflict
	}
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_proposal_qualification_policy_snapshot($1,$2,$3,$4,$5)", parent.request.TenantID, s.ProjectID, who.UserID, who.SessionID, parent.policy.body).Scan(&ready); err != nil {
		return zero, err
	}
	if !ready {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	outcome, run, err := v2input.BeginAdmissionInTransaction(ctx, tx, rebuilt.shell, rebuilt.key, parent.request.RequestSHA)
	if err != nil {
		return zero, err
	}
	if run != rebuilt.shell.Run || (outcome != "created" && outcome != "pending") {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return DraftAdmission{RunID: run, Outcome: outcome}, nil
}

func marshalNativeFaceDraftPins(ctx context.Context, plan nativeFaceDraftPlan) ([]byte, []byte, error) {
	if err := verifyNativeFacePlanSeal(ctx, plan); err != nil {
		return nil, nil, err
	}
	return marshalNativeFacePreparationPins(ctx, plan.prepared)
}

func marshalNativeFacePreparationPins(ctx context.Context, prepared nativeFaceTaskPreparation) ([]byte, []byte, error) {
	byteCounter, ok := prepared.frozen.(interface{ ParamsByteCount() (int64, error) })
	if !ok {
		return nil, nil, v2input.ErrInvalid
	}
	size, err := byteCounter.ParamsByteCount()
	if err != nil {
		return nil, nil, err
	}
	var inputs []v2draft.InputEvidence
	if json.Unmarshal([]byte(prepared.inputs), &inputs) != nil {
		return nil, nil, v2input.ErrInvalid
	}
	var params []byte
	for _, entry := range inputs {
		if entry.Contract != "Params" {
			continue
		}
		obj := entry.Object
		if params != nil || entry.Ref != prepared.params || obj.SHA256 != entry.Ref.SHA256 || obj.Bytes != size || obj.ContentType != "application/json" || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" {
			return nil, nil, v2input.ErrInvalid
		}
		params, err = canonical.Marshal(map[string]any{"key": obj.Key, "version": obj.VersionID, "sha256": obj.SHA256, "bytes": obj.Bytes, "media_type": obj.ContentType, "bucket": prepared.bucket})
		if err != nil {
			return nil, nil, err
		}
	}
	if len(params) == 0 || len(params) > 16384 {
		return nil, nil, v2input.ErrInvalid
	}
	q := prepared.report
	report, err := canonical.Marshal(personEvidencePin{q.contract, q.ref.ArtifactID, q.ref.SHA256, prepared.bucket, q.object.Key, q.object.VersionID, q.object.Bytes, q.backend})
	if err != nil || len(report) > 16384 {
		return nil, nil, v2input.ErrInvalid
	}
	return params, report, ctx.Err()
}

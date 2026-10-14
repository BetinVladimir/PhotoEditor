package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type defectPolicyExecutionPreparation struct {
	execution defectMaskExecutionPreparation
	policy    qualificationPolicySnapshot
}

// Owns inputs before catalog IO and retains the original full policy snapshot
// across all source/producer reads. Preparation grants no queue authority.
func (s SQLTransferSourceReader) preparePolicyQualifiedDefectExecutionEvidence(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, pins DefectOriginPins, faceReports, maskReports [2]v2input.Ref, faceExpected, maskExpected [2]v2qualification.Expected, report v2input.Ref, expected v2qualification.Expected, published PublishedDefectParams, nodes []v2draft.LayerRef, costs v2input.SemanticQualifiedCosts, ram, vram, scratch, output int64) (defectPolicyExecutionPreparation, error) {
	var zero defectPolicyExecutionPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if published.Ref != pins.Params {
		return zero, v2input.ErrInvalid
	}
	if _, err := validateDefectProfileDescriptors(published, nodes); err != nil {
		return zero, err
	}
	if err := validateDefectProfileQualificationFamilies(expected, faceExpected, &maskExpected); err != nil {
		return zero, err
	}
	body, err := canonical.Marshal(struct {
		Published PublishedDefectParams
		Nodes     []v2draft.LayerRef
	}{published, nodes})
	if err != nil || len(body) > 4<<20 {
		return zero, v2input.ErrInvalid
	}
	var owned struct {
		Published PublishedDefectParams
		Nodes     []v2draft.LayerRef
	}
	if json.Unmarshal(body, &owned) != nil {
		return zero, v2input.ErrInvalid
	}
	expected = ownQualificationExpected(expected)
	for i := range faceExpected {
		faceExpected[i] = ownQualificationExpected(faceExpected[i])
		maskExpected[i] = ownQualificationExpected(maskExpected[i])
	}
	policy, err := s.readQualificationPolicySnapshot(ctx, who, tenant)
	if err != nil {
		return zero, err
	}
	defectBody, err := canonical.Marshal(expected)
	if err != nil {
		return zero, err
	}
	familiesBody, err := canonical.Marshal([4]v2qualification.Expected{faceExpected[0], faceExpected[1], maskExpected[0], maskExpected[1]})
	if err != nil {
		return zero, err
	}
	expectations := defectMaskExecutionPreparation{face: defectFaceExecutionPreparation{source: defectSourceExecutionPreparation{expected: string(defectBody)}}, expected: string(familiesBody)}
	if err := matchDefectExecutionPolicySnapshot(expectations, policy); err != nil {
		return zero, err
	}
	execution, err := s.prepareMaskQualifiedDefectExecutionEvidence(ctx, who, tenant, binding, color, pins, faceReports, maskReports, faceExpected, maskExpected, report, expected, owned.Published, owned.Nodes, costs, ram, vram, scratch, output)
	if err != nil {
		return zero, err
	}
	if err := matchDefectExecutionPolicySnapshot(execution, policy); err != nil {
		return zero, err
	}
	current, err := s.readQualificationPolicySnapshot(ctx, who, tenant)
	if err != nil {
		return zero, err
	}
	if current != policy {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return defectPolicyExecutionPreparation{execution: execution, policy: policy}, nil
}

func (s SQLTransferSourceReader) lockDefectPolicyExecutionInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, prepared defectPolicyExecutionPreparation) error {

	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil || !validQualificationPolicySnapshot(s, who, tenant, prepared.policy) {
		return v2input.ErrInvalid
	}
	if err := matchDefectExecutionPolicySnapshot(prepared.execution, prepared.policy); err != nil {
		return err
	}
	source := prepared.execution.face.source.source
	if err := s.lockDefectExecutionAuthorization(ctx, tx, who, tenant, binding, source); err != nil {
		return err
	}
	if err := s.lockDefectExecutionPolicyInventoryInTransaction(ctx, tx, who, tenant, binding, prepared.execution, prepared.policy); err != nil {
		return err
	}
	authority, err := s.marshalDefectSourceAuthority(ctx, who, tenant, binding, prepared)
	if err != nil {
		return err
	}
	var matches bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_source_authority($1,$2,$3,$4,$5,$6,$7,$8::jsonb)", tenant, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, string(authority)).Scan(&matches); err != nil {
		return err
	}
	if !matches {
		return v2input.ErrConflict
	}
	faces, err := s.marshalDefectFaceAuthority(ctx, who, tenant, binding, prepared.execution.face)
	if err != nil {
		return err
	}
	parsers, err := s.marshalDefectParserAuthority(ctx, who, tenant, binding, prepared.execution)
	if err != nil {
		return err
	}
	manual, err := s.marshalDefectManualAuthority(ctx, who, tenant, binding, prepared.execution)
	if err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_selected_producers($1,$2,$3,$4,$5,$6,$7,$8::jsonb,$9::jsonb,$10::jsonb)", tenant, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, string(faces), string(parsers), string(manual)).Scan(&matches); err != nil {
		return err
	}
	if !matches {
		return v2input.ErrConflict
	}
	return s.lockDefectExecutionAuthorization(ctx, tx, who, tenant, binding, source)
}

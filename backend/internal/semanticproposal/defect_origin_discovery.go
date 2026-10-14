package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// The server pins the original prerequisite/cache task and exact Params SHA.
// A SHA alone cannot distinguish independently produced results. No newest-result
// preference. Returns only catalog provenance; full bytes, scientific policy,
// independent person protection and locked admission must still be verified.
func (s SQLTransferSourceReader) discoverDefectOriginForProducer(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, producerID uuid.UUID, paramsSHA string) (DefectOriginPins, error) {
	var zero DefectOriginPins
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if producerID == uuid.Nil || s.DB == nil || s.ProjectID == uuid.Nil || tenantID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || binding.AssetVersionID == uuid.Nil || binding.Source.ArtifactID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	for _, digest := range []string{binding.Source.SHA256, binding.CanvasHash, paramsSHA} {
		if _, err := domain.ParseDigest(digest); err != nil {
			return zero, v2input.ErrInvalid
		}
	}
	var selected DefectOriginPins
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT proposal.id,proposal.sha256,report.id,report.sha256,
labels.id,labels.sha256,protection.id,protection.sha256,params.id,params.sha256
FROM photo.artifacts params
JOIN photo.tasks producer ON producer.tenant_id=params.tenant_id AND producer.params_artifact_id=params.id
JOIN photo.artifacts proposal ON proposal.tenant_id=producer.tenant_id AND proposal.created_by_task_id=producer.id AND proposal.output_role='defect_candidates'
JOIN photo.artifacts report ON report.tenant_id=producer.tenant_id AND report.created_by_task_id=producer.id AND report.output_role='defect_propose_report'
JOIN photo.artifacts labels ON labels.tenant_id=producer.tenant_id AND labels.created_by_task_id=producer.id AND labels.output_role='defect_labels'
JOIN photo.artifacts protection ON protection.tenant_id=producer.tenant_id AND protection.created_by_task_id=producer.id AND protection.output_role='defect_protection'
WHERE params.tenant_id=$1 AND params.sha256=$8 AND producer.id=$9
AND photo.semantic_proposal_defect_origin($1,$2,$3,$4,$5,$6,$7,
proposal.id,proposal.sha256,report.id,report.sha256,labels.id,labels.sha256,
protection.id,protection.sha256,params.id,params.sha256)
ORDER BY proposal.id,proposal.sha256,report.id,report.sha256,labels.id,labels.sha256,
protection.id,protection.sha256,params.id,params.sha256 LIMIT 2`, tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, paramsSHA, producerID)
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			var pins DefectOriginPins
			if err := rows.Scan(&pins.Proposal.ArtifactID, &pins.Proposal.SHA256, &pins.Report.ArtifactID, &pins.Report.SHA256, &pins.Labels.ArtifactID, &pins.Labels.SHA256, &pins.Protection.ArtifactID, &pins.Protection.SHA256, &pins.Params.ArtifactID, &pins.Params.SHA256); err != nil {
				return err
			}
			seen := map[uuid.UUID]bool{}
			for _, ref := range []v2input.Ref{pins.Proposal, pins.Report, pins.Labels, pins.Protection, pins.Params} {
				if ref.ArtifactID == uuid.Nil || seen[ref.ArtifactID] {
					return v2input.ErrInvalid
				}
				if _, err := domain.ParseDigest(ref.SHA256); err != nil {
					return v2input.ErrInvalid
				}
				seen[ref.ArtifactID] = true
			}
			if pins.Params.SHA256 != paramsSHA {
				return v2input.ErrConflict
			}
			selected = pins
			count++
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if count == 0 {
			return v2input.ErrInvalid
		}
		if count != 1 {
			return v2input.ErrConflict
		}
		return nil
	})
	if err != nil {
		return zero, err
	}
	if err := s.VerifyDefectOrigin(ctx, who, tenantID, binding, selected); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return selected, nil
}

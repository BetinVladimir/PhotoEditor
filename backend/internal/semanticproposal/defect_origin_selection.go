package semanticproposal

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// A selection identifies an execution, not a scientific acceptance or an
// admission permission. Persisting it still requires the existing locked gates.
type defectOriginSelection struct {
	ProducerID uuid.UUID `json:"producer_task_id"`
	ParamsSHA  string    `json:"params_sha256"`
}

// Fail before admission SQL if an owned plan's saved locator drifts
// from its privately retained original selection. Historical configs may omit
// the locator, but cannot supply automatic restart selection.
func validatePreparedDefectSelection(config []byte, selected defectOriginSelection, originalParams v2input.Ref) error {
	if len(config) < 2 || len(config) > 1<<20 || selected.ProducerID == uuid.Nil || originalParams.ArtifactID == uuid.Nil || selected.ParamsSHA != originalParams.SHA256 {
		return v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(selected.ParamsSHA); err != nil {
		return v2input.ErrInvalid
	}
	var root map[string]json.RawMessage
	if json.Unmarshal(config, &root) != nil {
		return v2input.ErrInvalid
	}
	var native map[string]json.RawMessage
	if json.Unmarshal(root["v27_defect_prepare"], &native) != nil || native == nil {
		return v2input.ErrInvalid
	}
	raw, exists := native["original_producer"]
	if !exists {
		return nil
	}
	var fields map[string]json.RawMessage
	var saved defectOriginSelection
	if json.Unmarshal(raw, &fields) != nil || len(fields) != 2 || fields["producer_task_id"] == nil || fields["params_sha256"] == nil || json.Unmarshal(raw, &saved) != nil {
		return v2input.ErrInvalid
	}
	if saved != selected {
		return v2input.ErrConflict
	}
	return nil
}

// Recover the producer from a fully scoped original result, then require that
// discovering that producer yields exactly every supplied pin. Never select an
// execution by Params SHA alone: independent executions can share Params.
func (s SQLTransferSourceReader) selectDefectOriginProducer(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins) (defectOriginSelection, error) {
	var zero defectOriginSelection
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.ProjectID == uuid.Nil || tenantID == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || binding.AssetVersionID == uuid.Nil || binding.Source.ArtifactID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{}
	for _, ref := range []v2input.Ref{pins.Proposal, pins.Report, pins.Labels, pins.Protection, pins.Params, binding.Source} {
		if ref.ArtifactID == uuid.Nil || seen[ref.ArtifactID] {
			return zero, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(ref.SHA256); err != nil {
			return zero, v2input.ErrInvalid
		}
		seen[ref.ArtifactID] = true
	}
	if _, err := domain.ParseDigest(binding.CanvasHash); err != nil {
		return zero, v2input.ErrInvalid
	}
	var producer uuid.UUID
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, `SELECT created_by_task_id FROM photo.artifacts
WHERE tenant_id=$1 AND id=$8 AND sha256=$9 AND output_role='defect_candidates'
AND photo.semantic_proposal_defect_origin($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`, tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, pins.Proposal.ArtifactID, pins.Proposal.SHA256, pins.Report.ArtifactID, pins.Report.SHA256, pins.Labels.ArtifactID, pins.Labels.SHA256, pins.Protection.ArtifactID, pins.Protection.SHA256, pins.Params.ArtifactID, pins.Params.SHA256).Scan(&producer)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrInvalid
	}
	if err != nil {
		return zero, err
	}
	selected, err := s.discoverDefectOriginForProducer(ctx, who, tenantID, binding, producer, pins.Params.SHA256)
	if err != nil {
		return zero, err
	}
	if selected != pins {
		return zero, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return defectOriginSelection{ProducerID: producer, ParamsSHA: pins.Params.SHA256}, nil
}

package interchange

import (
	"context"

	"photoeditor/backend/internal/v2input"
)

// SchedulerExportPersonOriginCurrentChecker binds each signed receipt to the
// current canonical mask.person producer under the export adoption lease.
type SchedulerExportPersonOriginCurrentChecker struct {
	Claims SQLExportAdoptionClaimStore
	Claim  ExportAdoptionClaim
}

func (s SchedulerExportPersonOriginCurrentChecker) RecheckExportPersonOrigin(ctx context.Context,
	claim PortablePersonOriginClaim) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || !validPersonOriginClaim(claim) {
		return v2input.ErrInvalid
	}
	var current bool
	err := s.Claims.DB.QueryRow(ctx, `SELECT photo.v30_export_adoption_person_origin_current(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task, s.Claim.Result,
		s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA, s.Claim.Token,
		claim.Source.AssetVersionID, claim.Source.Source.ArtifactID, claim.Source.Source.SHA256,
		claim.MaskRef.ArtifactID, claim.MaskRef.SHA256,
		claim.MaskPixels.ArtifactID, claim.MaskPixels.SHA256,
		claim.ProducerTaskID, claim.ProducerAttempt, claim.ProducerFence,
		claim.QualificationReport.ArtifactID, claim.QualificationReport.SHA256).Scan(&current)
	if err != nil {
		return err
	}
	if !current {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

var _ ExportPersonOriginCurrentChecker = SchedulerExportPersonOriginCurrentChecker{}

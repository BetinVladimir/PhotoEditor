package interchange

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// QualifyExportPersonProfile rechecks producer locks and live workflow consent
// for a frozen source and the exact person-mask origin.
func (s SQLSchedulerExportInputCatalog) QualifyExportPersonProfile(ctx context.Context,
	origin ExportStatsPersonOrigin, binding v2input.SessionVersionBinding,
	expected v2qualification.Expected) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil ||
		expected.Operation != "mask.person" || expected.Version != "1" ||
		!expected.LicenseCleared || !validImportRef(origin.Root) ||
		!validImportRef(origin.Params) || !validImportRef(origin.Report) ||
		!validImportRef(binding.Source) || binding.AssetVersionID == uuid.Nil ||
		origin.Mask.Key == "" || origin.Mask.VersionID == "" ||
		origin.Mask.VersionID == "null" || !validImportRefForSHA(origin.Mask.SHA256) ||
		origin.Mask.Bytes < 1 || !exportArrayMediaType(origin.Mask.ContentType) {
		return v2input.ErrInvalid
	}
	policy, err := canonical.Marshal(expected)
	if err != nil {
		return err
	}
	var allowed bool
	err = s.Claims.DB.QueryRow(ctx, `SELECT photo.v30_export_adoption_person_profile(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,
 $17,$18,$19,$20,$21,$22,$23,$24::jsonb,$25)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, origin.Root.ArtifactID, origin.Root.SHA256,
		origin.Params.ArtifactID, origin.Params.SHA256,
		origin.Report.ArtifactID, origin.Report.SHA256,
		origin.Mask.SHA256, origin.Mask.Key, origin.Mask.VersionID,
		origin.Mask.Bytes, origin.Mask.ContentType,
		binding.Source.ArtifactID, binding.Source.SHA256,
		binding.AssetVersionID, string(policy), s.Bucket).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return v2qualification.ErrUnqualified
	}
	return ctx.Err()
}

package interchange

import (
	"context"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// QualifyExportPromptProfile rechecks the canonical prompt-mask attempt and
// consent epochs after operator qualification bytes have been verified.
func (s SQLSchedulerExportInputCatalog) QualifyExportPromptProfile(ctx context.Context,
	origin ExportSeriesMaskOrigin, binding v2input.SessionVersionBinding,
	expected v2qualification.Expected) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil ||
		origin.Operation != "mask.prompt" || origin.Version != "1" ||
		expected.Operation != "mask.prompt" || expected.Version != "1" ||
		!expected.LicenseCleared || !validImportRef(origin.Root.Ref) ||
		!validImportRef(origin.Params.Ref) || !validImportRef(origin.Report.Ref) ||
		!validImportRef(binding.Source) || origin.Attempt < 1 || origin.Fence < 1 ||
		origin.Mask.Key == "" ||
		origin.Mask.VersionID == "" || origin.Mask.VersionID == "null" ||
		origin.Params.Object.Key == "" || origin.Params.Object.VersionID == "" ||
		origin.Params.Object.VersionID == "null" || origin.Params.Object.SHA256 != origin.Params.Ref.SHA256 {
		return v2input.ErrInvalid
	}
	policy, err := canonical.Marshal(expected)
	if err != nil {
		return err
	}
	var allowed bool
	err = s.Claims.DB.QueryRow(ctx, `SELECT photo.v30_export_adoption_prompt_profile(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,
 $17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27,$28,$29::jsonb,$30)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, origin.Task, origin.Attempt, origin.Fence,
		origin.Root.Ref.ArtifactID, origin.Root.Ref.SHA256,
		origin.Report.Ref.ArtifactID, origin.Report.Ref.SHA256,
		origin.Params.Ref.ArtifactID, origin.Params.Ref.SHA256,
		origin.Params.Object.Key, origin.Params.Object.VersionID,
		origin.Params.Object.Bytes, origin.Mask.Key, origin.Mask.VersionID,
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

var _ ExportPromptPolicyReader = SQLSchedulerExportInputCatalog{}

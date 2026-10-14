package interchange

import (
	"context"

	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// QualifyExportMaskArray binds a physical MaskPixels object to an exact
// MaskRef edge and its current source-scoped canonical producer.
func (s SQLSchedulerExportInputCatalog) QualifyExportMaskArray(ctx context.Context,
	parent v2input.Entry, raw []byte, array v2input.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || parent.Contract != "MaskRef" ||
		array.Contract != "MaskPixels" || !validImportRef(parent.Ref) ||
		!validImportRef(array.Ref) || len(raw) < 2 || len(raw) > 1<<20 ||
		rawSHA(raw) != parent.Ref.SHA256 {
		return v2input.ErrInvalid
	}
	mask, err := review.ParseMaskRef(raw)
	if err != nil {
		return err
	}
	if mask.Artifact.ID != array.Ref.ArtifactID || mask.Artifact.SHA256 != array.Ref.SHA256 {
		return v2input.ErrInvalid
	}
	beforeParent, err := s.LookupExportDocument(ctx, parent)
	if err != nil {
		return err
	}
	beforeArray, err := s.LookupExportDocument(ctx, array)
	if err != nil {
		return err
	}
	if beforeParent.ContentType != "application/json" || !exportArrayMediaType(beforeArray.ContentType) {
		return v2input.ErrInvalid
	}
	var allowed bool
	err = s.Claims.DB.QueryRow(ctx, `SELECT photo.v30_export_adoption_mask_array(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task, s.Claim.Result,
		s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA, s.Claim.Token,
		parent.Ref.ArtifactID, parent.Ref.SHA256, array.Ref.ArtifactID,
		array.Ref.SHA256).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return v2input.ErrInvalid
	}
	afterParent, err := s.LookupExportDocument(ctx, parent)
	if err != nil || afterParent != beforeParent {
		return v2input.ErrInvalid
	}
	afterArray, err := s.LookupExportDocument(ctx, array)
	if err != nil || afterArray != beforeArray {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

var _ ExportMaskArrayAuthority = SQLSchedulerExportInputCatalog{}

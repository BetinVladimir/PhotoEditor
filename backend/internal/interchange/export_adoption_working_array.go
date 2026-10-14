package interchange

import (
	"context"

	"photoeditor/backend/internal/v2input"
)

// QualifyExportWorkingArray binds the physically checked array to its current
// WorkingImage producer and the export's frozen snapshot, without a session.
func (s SQLSchedulerExportInputCatalog) QualifyExportWorkingArray(ctx context.Context,
	parent, array v2input.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || parent.Contract != "WorkingImage" ||
		(array.Contract != "WorkingPixels" && array.Contract != "WorkingAlpha") ||
		!validImportRef(parent.Ref) || !validImportRef(array.Ref) ||
		parent.Ref.ArtifactID == array.Ref.ArtifactID {
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
	var allowed bool
	err = s.Claims.DB.QueryRow(ctx, `SELECT photo.v30_export_adoption_working_array(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task, s.Claim.Result,
		s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA, s.Claim.Token,
		parent.Ref.ArtifactID, parent.Ref.SHA256, array.Ref.ArtifactID,
		array.Ref.SHA256, array.Contract).Scan(&allowed)
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

var _ ExportWorkingArrayAuthority = SQLSchedulerExportInputCatalog{}

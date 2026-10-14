package interchange

import (
	"context"

	"photoeditor/backend/internal/v2input"
)

// QualifyExportDenseWarpOccurrence proves the JSON edge, sealed input pins,
// frozen source and the field's current canonical producer. MaskRef-origin
// occurrences remain closed until their independent scheduler authority exists.
func (s SQLSchedulerExportInputCatalog) QualifyExportDenseWarpOccurrence(ctx context.Context,
	parent v2input.Entry, raw []byte, index int, field v2input.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || parent.Contract != "WorkingImage" {
		return v2input.ErrInvalid
	}
	if err := validateExportDenseWarpOccurrence(parent, raw, index, field); err != nil {
		return err
	}
	beforeParent, err := s.LookupExportDocument(ctx, parent)
	if err != nil {
		return err
	}
	beforeField, err := s.LookupExportDocument(ctx, field)
	if err != nil {
		return err
	}
	if beforeParent.ContentType != "application/json" || !exportArrayMediaType(beforeField.ContentType) {
		return v2input.ErrInvalid
	}
	var allowed bool
	err = s.Claims.DB.QueryRow(ctx, `SELECT photo.v30_export_adoption_working_warp(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task, s.Claim.Result,
		s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA, s.Claim.Token,
		parent.Ref.ArtifactID, parent.Ref.SHA256, field.Ref.ArtifactID,
		field.Ref.SHA256).Scan(&allowed)
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
	afterField, err := s.LookupExportDocument(ctx, field)
	if err != nil || afterField != beforeField {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

var _ ExportDenseWarpOccurrenceAuthority = SQLSchedulerExportInputCatalog{}

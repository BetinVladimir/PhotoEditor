package interchange

import (
	"context"
	"encoding/json"

	"photoeditor/backend/internal/v2input"
)

// QualifyExportLUT binds a resolved-style LUT to its current completed
// style.resolve producer and the export's frozen source snapshot.
func (s SQLSchedulerExportInputCatalog) QualifyExportLUT(ctx context.Context,
	parent v2input.Entry, raw []byte, array v2input.Entry) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || parent.Contract != "ResolvedStyle" ||
		array.Contract != "AbsoluteLUTArray" || !validImportRef(parent.Ref) ||
		!validImportRef(array.Ref) || len(raw) < 2 || len(raw) > 8<<20 ||
		rawSHA(raw) != parent.Ref.SHA256 {
		return v2input.ErrInvalid
	}
	var body struct {
		Params struct {
			Image v2input.Ref `json:"image"`
		} `json:"params"`
		LUT *struct {
			Artifact v2input.Ref `json:"artifact"`
		} `json:"absolute_lut"`
	}
	if json.Unmarshal(raw, &body) != nil || body.LUT == nil ||
		body.LUT.Artifact != array.Ref || !validImportRef(body.Params.Image) {
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
	err = s.Claims.DB.QueryRow(ctx, `SELECT photo.v30_export_adoption_lut_origin(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task, s.Claim.Result,
		s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA, s.Claim.Token,
		body.Params.Image.ArtifactID, body.Params.Image.SHA256,
		parent.Ref.ArtifactID, parent.Ref.SHA256,
		array.Ref.ArtifactID, array.Ref.SHA256).Scan(&allowed)
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

var _ ExportLUTOccurrenceAuthority = SQLSchedulerExportInputCatalog{}

package interchange

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func (s SQLSchedulerExportInputCatalog) QualifyExportSeriesMeasureInputs(ctx context.Context,
	origin ExportSeriesMeasureOrigin, stat v2input.Entry, raw []byte, image ExportClosureNode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || stat.Contract != "SeriesStats" ||
		!validImportRef(stat.Ref) || origin.Task == uuid.Nil ||
		origin.Attempt < 1 || origin.Fence < 1 || !validImportRef(origin.Params) ||
		origin.Object.Key == "" || origin.Object.VersionID == "" ||
		origin.Object.VersionID == "null" ||
		origin.Object.SHA256 != origin.Params.SHA256 {
		return v2input.ErrInvalid
	}
	refs, err := exportSeriesMeasureInputRefs(origin, raw, image)
	if err != nil {
		return err
	}
	inputs, err := canonical.Marshal(refs)
	if err != nil {
		return err
	}
	before, err := s.LookupExportDocument(ctx, image.Entry)
	if err != nil {
		return err
	}
	var allowed bool
	err = s.Claims.DB.QueryRow(ctx, `SELECT photo.v30_export_adoption_series_measure_inputs(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21::jsonb)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, origin.Task, origin.Attempt, origin.Fence,
		stat.Ref.ArtifactID, stat.Ref.SHA256,
		origin.Params.ArtifactID, origin.Params.SHA256, s.Bucket,
		origin.Object.Key, origin.Object.VersionID, origin.Object.Bytes, inputs).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return v2input.ErrInvalid
	}
	after, err := s.LookupExportDocument(ctx, image.Entry)
	if err != nil {
		return err
	}
	if after != before {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

var _ ExportSeriesMeasureInputAuthority = SQLSchedulerExportInputCatalog{}

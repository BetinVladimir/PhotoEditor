package interchange

import (
	"context"
	"encoding/json"

	"photoeditor/backend/internal/v2input"
)

func (s SQLSchedulerExportInputCatalog) QualifyExportSeriesStatImage(ctx context.Context,
	origin ExportSeriesOriginEvidence, entry v2input.Entry, raw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || entry.Contract != "SeriesStats" ||
		!validImportRef(entry.Ref) || len(raw) < 2 || len(raw) > 1<<20 ||
		rawSHA(raw) != entry.Ref.SHA256 || !validImportRef(origin.Params) ||
		origin.Object.SHA256 != origin.Params.SHA256 || origin.Object.Key == "" ||
		origin.Object.VersionID == "" || origin.Object.VersionID == "null" {
		return v2input.ErrInvalid
	}
	var body struct {
		Image v2input.Ref `json:"image"`
	}
	if json.Unmarshal(raw, &body) != nil || !validImportRef(body.Image) {
		return v2input.ErrInvalid
	}
	stat, err := s.LookupExportSeriesOriginDocument(ctx, origin, entry)
	if err != nil {
		return err
	}
	image := v2input.Entry{Ref: body.Image, Contract: "WorkingImage"}
	imageObject, err := s.LookupExportDocument(ctx, image)
	if err != nil {
		return err
	}
	var allowed bool
	err = s.Claims.DB.QueryRow(ctx, `SELECT photo.v30_export_adoption_series_stat_image(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, origin.Task, origin.Attempt, origin.Fence,
		origin.Params.ArtifactID, origin.Params.SHA256, s.Bucket,
		origin.Object.Key, origin.Object.VersionID, origin.Object.Bytes,
		entry.Ref.ArtifactID, entry.Ref.SHA256,
		body.Image.ArtifactID, body.Image.SHA256).Scan(&allowed)
	if err != nil {
		return err
	}
	if !allowed {
		return v2input.ErrInvalid
	}
	currentStat, err := s.LookupExportSeriesOriginDocument(ctx, origin, entry)
	if err != nil {
		return err
	}
	currentImage, err := s.LookupExportDocument(ctx, image)
	if err != nil {
		return err
	}
	if currentStat != stat || currentImage != imageObject {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

var _ ExportSeriesStatImageAuthority = SQLSchedulerExportInputCatalog{}

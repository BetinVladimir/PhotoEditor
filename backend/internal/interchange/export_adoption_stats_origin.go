package interchange

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func (s SQLSchedulerExportInputCatalog) LookupExportStatsOrigin(ctx context.Context,
	entry v2input.Entry, raw []byte) (ExportStatsOriginEvidence, error) {
	var zero, evidence ExportStatsOriginEvidence
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || entry.Contract != "Stats20" ||
		!validImportRef(entry.Ref) || len(raw) < 2 || len(raw) > 8<<20 ||
		rawSHA(raw) != entry.Ref.SHA256 {
		return zero, v2input.ErrInvalid
	}
	stats, err := review.ParseStats20(raw)
	if err != nil || stats.SourceImage == nil {
		return zero, v2input.ErrInvalid
	}
	source := v2input.Entry{Contract: "WorkingImage", Ref: v2input.Ref{
		ArtifactID: stats.SourceImage.ID, SHA256: stats.SourceImage.SHA256}}
	if !validImportRef(source.Ref) || source.Ref == entry.Ref {
		return zero, v2input.ErrInvalid
	}
	statsObject, err := s.LookupExportDocument(ctx, entry)
	if err != nil {
		return zero, err
	}
	imageObject, err := s.LookupExportDocument(ctx, source)
	if err != nil {
		return zero, err
	}
	if statsObject.ContentType != "application/json" || imageObject.ContentType != "application/json" {
		return zero, v2input.ErrInvalid
	}
	var bucket string
	err = s.Claims.DB.QueryRow(ctx, `SELECT params_id,params_sha,params_bucket,params_key,
 params_version,params_sha,params_bytes,params_type
 FROM photo.v30_export_adoption_stats_origin($1,$2,$3,$4,$5,$6,$7,$8,$9,
 $10,$11,$12,$13,$14,$15,$16,$17)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, entry.Ref.ArtifactID, entry.Ref.SHA256,
		source.Ref.ArtifactID, source.Ref.SHA256,
		s.Bucket, statsObject.Key, statsObject.VersionID, statsObject.Bytes).Scan(
		&evidence.Params.ArtifactID, &evidence.Params.SHA256, &bucket,
		&evidence.Object.Key, &evidence.Object.VersionID,
		&evidence.Object.SHA256, &evidence.Object.Bytes, &evidence.Object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrInvalid
	}
	if err != nil {
		return zero, err
	}
	if !validImportRef(evidence.Params) || bucket != s.Bucket ||
		evidence.Object.Key == "" || evidence.Object.VersionID == "" ||
		evidence.Object.VersionID == "null" ||
		evidence.Object.SHA256 != evidence.Params.SHA256 ||
		evidence.Object.Bytes < 2 || evidence.Object.Bytes > 1<<20 ||
		evidence.Object.ContentType != "application/json" {
		return zero, v2input.ErrInvalid
	}
	currentStats, err := s.LookupExportDocument(ctx, entry)
	if err != nil {
		return zero, err
	}
	currentImage, err := s.LookupExportDocument(ctx, source)
	if err != nil {
		return zero, err
	}
	if currentStats != statsObject || currentImage != imageObject {
		return zero, v2input.ErrInvalid
	}
	return evidence, ctx.Err()
}

var _ ExportStatsOriginSource = SQLSchedulerExportInputCatalog{}

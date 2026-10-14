package interchange

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

func (s SQLSchedulerExportInputCatalog) LookupExportSeriesMeasureOrigin(ctx context.Context,
	balance ExportSeriesOriginEvidence, entry v2input.Entry, raw []byte) (ExportSeriesMeasureOrigin, error) {
	var zero, origin ExportSeriesMeasureOrigin
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || entry.Contract != "SeriesStats" ||
		!validImportRef(entry.Ref) || len(raw) < 2 || len(raw) > 1<<20 ||
		rawSHA(raw) != entry.Ref.SHA256 || balance.Task == uuid.Nil ||
		balance.Attempt < 1 || balance.Fence < 1 || !validImportRef(balance.Params) ||
		balance.Object.SHA256 != balance.Params.SHA256 ||
		balance.Object.Key == "" || balance.Object.VersionID == "" ||
		balance.Object.VersionID == "null" {
		return zero, v2input.ErrInvalid
	}
	var stat struct {
		Image v2input.Ref `json:"image"`
	}
	if json.Unmarshal(raw, &stat) != nil || !validImportRef(stat.Image) {
		return zero, v2input.ErrInvalid
	}
	if err := s.QualifyExportSeriesStatImage(ctx, balance, entry, raw); err != nil {
		return zero, err
	}
	statObject, err := s.LookupExportSeriesOriginDocument(ctx, balance, entry)
	if err != nil {
		return zero, err
	}
	var bucket string
	err = s.Claims.DB.QueryRow(ctx, `SELECT producer_task,producer_attempt,producer_fence,
 params_id,params_sha,params_bucket,params_key,params_version,params_sha,params_bytes,params_type
 FROM photo.v30_export_adoption_series_measure_origin($1,$2,$3,$4,$5,$6,$7,$8,$9,
 $10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, balance.Task, balance.Attempt, balance.Fence,
		balance.Params.ArtifactID, balance.Params.SHA256, s.Bucket,
		balance.Object.Key, balance.Object.VersionID, balance.Object.Bytes,
		entry.Ref.ArtifactID, entry.Ref.SHA256, statObject.Key,
		statObject.VersionID, statObject.Bytes).Scan(&origin.Task,
		&origin.Attempt, &origin.Fence, &origin.Params.ArtifactID,
		&origin.Params.SHA256, &bucket, &origin.Object.Key,
		&origin.Object.VersionID, &origin.Object.SHA256,
		&origin.Object.Bytes, &origin.Object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrInvalid
	}
	if err != nil {
		return zero, err
	}
	if origin.Task == uuid.Nil || origin.Attempt < 1 || origin.Fence < 1 ||
		!validImportRef(origin.Params) || bucket != s.Bucket ||
		origin.Object.Key == "" || origin.Object.VersionID == "" ||
		origin.Object.VersionID == "null" ||
		origin.Object.SHA256 != origin.Params.SHA256 ||
		origin.Object.Bytes < 2 || origin.Object.Bytes > 1<<20 ||
		origin.Object.ContentType != "application/json" {
		return zero, v2input.ErrInvalid
	}
	if err := s.QualifyExportSeriesStatImage(ctx, balance, entry, raw); err != nil {
		return zero, err
	}
	current, err := s.LookupExportSeriesOriginDocument(ctx, balance, entry)
	if err != nil {
		return zero, err
	}
	if current != statObject {
		return zero, v2input.ErrInvalid
	}
	return origin, ctx.Err()
}

var _ ExportSeriesMeasureOriginSource = SQLSchedulerExportInputCatalog{}

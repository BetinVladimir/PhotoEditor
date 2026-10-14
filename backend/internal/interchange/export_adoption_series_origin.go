package interchange

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func (s SQLSchedulerExportInputCatalog) LookupExportSeriesOrigin(ctx context.Context,
	entry v2input.Entry, raw []byte) (ExportSeriesOriginEvidence, error) {
	var zero, origin ExportSeriesOriginEvidence
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || entry.Contract != "SeriesCorrections" ||
		!validImportRef(entry.Ref) || len(raw) < 2 || len(raw) > 8<<20 || rawSHA(raw) != entry.Ref.SHA256 {
		return zero, v2input.ErrInvalid
	}
	if err := s.QualifyExportSeries(ctx, entry, raw); err != nil {
		return zero, err
	}
	object, err := s.LookupExportDocument(ctx, entry)
	if err != nil {
		return zero, err
	}
	var bucket string
	err = s.Claims.DB.QueryRow(ctx, `SELECT producer_task,producer_attempt,producer_fence,
 params_id,params_sha,params_bucket,params_key,params_version,params_sha,params_bytes,params_type
 FROM photo.v30_export_adoption_series_origin($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, entry.Ref.ArtifactID, entry.Ref.SHA256,
		s.Bucket, object.Key, object.VersionID, object.Bytes).Scan(
		&origin.Task, &origin.Attempt, &origin.Fence,
		&origin.Params.ArtifactID, &origin.Params.SHA256, &bucket,
		&origin.Object.Key, &origin.Object.VersionID, &origin.Object.SHA256,
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
	if err := s.QualifyExportSeries(ctx, entry, raw); err != nil {
		return zero, err
	}
	current, err := s.LookupExportDocument(ctx, entry)
	if err != nil {
		return zero, err
	}
	if current != object {
		return zero, v2input.ErrInvalid
	}
	return origin, ctx.Err()
}

func (s SQLSchedulerExportInputCatalog) LookupExportSeriesOriginDocument(ctx context.Context,
	origin ExportSeriesOriginEvidence, entry v2input.Entry) (objectstore.Object, error) {
	var zero, object objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || s.Bucket == "" ||
		entry.Contract != "SeriesStats" || !validImportRef(entry.Ref) ||
		origin.Task == uuid.Nil || origin.Attempt < 1 || origin.Fence < 1 ||
		!validImportRef(origin.Params) || origin.Object.Key == "" ||
		origin.Object.VersionID == "" || origin.Object.VersionID == "null" ||
		origin.Object.SHA256 != origin.Params.SHA256 ||
		origin.Object.Bytes < 2 || origin.Object.Bytes > 1<<20 {
		return zero, v2input.ErrInvalid
	}
	current, err := s.Claims.CurrentExportAdoptionClaim(ctx, s.Claim)
	if err != nil {
		return zero, err
	}
	if !current {
		return zero, v2input.ErrInvalid
	}
	var bucket string
	err = s.Claims.DB.QueryRow(ctx, `SELECT bucket,object_key,object_version,sha256,bytes,media_type
 FROM photo.v30_export_adoption_series_stat($1,$2,$3,$4,$5,$6,$7,$8,$9,
 $10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, origin.Task, origin.Attempt, origin.Fence,
		origin.Params.ArtifactID, origin.Params.SHA256, s.Bucket,
		origin.Object.Key, origin.Object.VersionID, origin.Object.Bytes,
		entry.Ref.ArtifactID, entry.Ref.SHA256).Scan(&bucket, &object.Key,
		&object.VersionID, &object.SHA256, &object.Bytes, &object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrInvalid
	}
	if err != nil {
		return zero, err
	}
	if bucket != s.Bucket || object.Key == "" || object.VersionID == "" ||
		object.VersionID == "null" || object.SHA256 != entry.Ref.SHA256 ||
		object.Bytes < 2 || object.Bytes > 1<<20 || object.ContentType != "application/json" {
		return zero, v2input.ErrInvalid
	}
	current, err = s.Claims.CurrentExportAdoptionClaim(ctx, s.Claim)
	if err != nil {
		return zero, err
	}
	if !current {
		return zero, v2input.ErrInvalid
	}
	return object, ctx.Err()
}

var _ ExportSeriesOriginSource = SQLSchedulerExportInputCatalog{}

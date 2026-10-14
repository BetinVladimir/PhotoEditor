package interchange

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func (s SQLSchedulerExportInputCatalog) LookupExportStatsPersonOrigin(ctx context.Context,
	entry v2input.Entry, paramsRef v2input.Ref, raw []byte,
	image review.WorkingImage) (ExportStatsPersonOrigin, error) {
	var zero, origin ExportStatsPersonOrigin
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || entry.Contract != "Stats20" ||
		!validImportRef(entry.Ref) || !validImportRef(paramsRef) ||
		len(raw) < 2 || len(raw) > 1<<20 || rawSHA(raw) != paramsRef.SHA256 {
		return zero, v2input.ErrInvalid
	}
	params, err := review.ParseStatsParams(raw)
	if err != nil {
		return zero, err
	}
	imageRaw, err := canonical.Marshal(image)
	if err != nil {
		return zero, err
	}
	owned, err := review.ParseWorkingImage(imageRaw)
	if err != nil || rawSHA(imageRaw) != params.Image.SHA256 {
		return zero, v2input.ErrInvalid
	}
	if err := params.ValidateSource(owned, params.Image); err != nil {
		return zero, err
	}
	imageRef := v2input.Ref{ArtifactID: params.Image.ID, SHA256: params.Image.SHA256}
	maskRef := v2input.Ref{ArtifactID: params.Person.Artifact.ID,
		SHA256: params.Person.Artifact.SHA256}
	if !validImportRef(imageRef) || !validImportRef(maskRef) {
		return zero, v2input.ErrInvalid
	}
	statsObject, err := s.LookupExportDocument(ctx, entry)
	if err != nil {
		return zero, err
	}
	paramsEntry := v2input.Entry{Ref: paramsRef, Contract: "Params"}
	paramsObject, err := s.LookupExportDocument(ctx, paramsEntry)
	if err != nil {
		return zero, err
	}
	imageEntry := v2input.Entry{Ref: imageRef, Contract: "WorkingImage"}
	imageObject, err := s.LookupExportDocument(ctx, imageEntry)
	if err != nil {
		return zero, err
	}
	if paramsObject.Bytes != int64(len(raw)) || imageObject.Bytes != int64(len(imageRaw)) {
		return zero, v2input.ErrInvalid
	}
	var bucket string
	err = s.Claims.DB.QueryRow(ctx, `SELECT root_id,root_sha,original_id,original_sha,
 report_id,report_sha,mask_bucket,mask_key,mask_version,mask_sha256,mask_bytes,mask_type
 FROM photo.v30_export_adoption_stats_person_origin($1,$2,$3,$4,$5,$6,$7,$8,$9,
 $10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, entry.Ref.ArtifactID, entry.Ref.SHA256,
		paramsRef.ArtifactID, paramsRef.SHA256,
		maskRef.ArtifactID, maskRef.SHA256,
		imageRef.ArtifactID, imageRef.SHA256, s.Bucket).Scan(
		&origin.Root.ArtifactID, &origin.Root.SHA256,
		&origin.Params.ArtifactID, &origin.Params.SHA256,
		&origin.Report.ArtifactID, &origin.Report.SHA256,
		&bucket, &origin.Mask.Key, &origin.Mask.VersionID,
		&origin.Mask.SHA256, &origin.Mask.Bytes, &origin.Mask.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrInvalid
	}
	if err != nil {
		return zero, err
	}
	if bucket != s.Bucket || !validImportRef(origin.Root) ||
		!validImportRef(origin.Params) || !validImportRef(origin.Report) ||
		origin.Root.ArtifactID == origin.Params.ArtifactID ||
		origin.Root.ArtifactID == origin.Report.ArtifactID ||
		origin.Params.ArtifactID == origin.Report.ArtifactID ||
		origin.Mask.Key == "" || origin.Mask.VersionID == "" ||
		origin.Mask.VersionID == "null" || origin.Mask.SHA256 != maskRef.SHA256 ||
		origin.Mask.Bytes < 1 || !exportArrayMediaType(origin.Mask.ContentType) {
		return zero, v2input.ErrInvalid
	}
	currentStats, err := s.LookupExportDocument(ctx, entry)
	if err != nil {
		return zero, err
	}
	currentParams, err := s.LookupExportDocument(ctx, paramsEntry)
	if err != nil {
		return zero, err
	}
	currentImage, err := s.LookupExportDocument(ctx, imageEntry)
	if err != nil {
		return zero, err
	}
	if currentStats != statsObject || currentParams != paramsObject ||
		currentImage != imageObject {
		return zero, v2input.ErrInvalid
	}
	return origin, ctx.Err()
}

var _ ExportStatsPersonOriginSource = SQLSchedulerExportInputCatalog{}

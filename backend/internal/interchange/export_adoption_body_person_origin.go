package interchange

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

func (s SQLSchedulerExportInputCatalog) LookupExportBodyPersonOrigin(ctx context.Context,
	parent v2input.Entry, raw []byte, image ExportClosureNode) (ExportStatsPersonOrigin, error) {
	var zero, out ExportStatsPersonOrigin
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil {
		return zero, v2input.ErrInvalid
	}
	projected, err := exportBodyPersonProjection(parent, raw, image)
	if err != nil {
		return zero, err
	}
	body := append([]byte(nil), raw...)
	consumer, err := s.LookupExportParamsOperation(ctx, parent, body)
	if err != nil {
		return zero, err
	}
	if consumer.Operation != "geometry.body" || consumer.Version != "1" {
		return zero, v2input.ErrInvalid
	}
	parentPin, err := s.LookupExportDocument(ctx, parent)
	if err != nil {
		return zero, err
	}
	imagePin, err := s.LookupExportDocument(ctx, image.Entry)
	if err != nil {
		return zero, err
	}
	if parentPin.Bytes != int64(len(body)) || imagePin.Bytes != int64(len(image.Body)) {
		return zero, v2input.ErrInvalid
	}
	mask := projected.Person
	var bucket string
	err = s.Claims.DB.QueryRow(ctx, `SELECT root_id,root_sha,original_id,original_sha,
 report_id,report_sha,mask_bucket,mask_key,mask_version,mask_sha256,mask_bytes,mask_type
 FROM photo.v30_export_adoption_body_person_origin(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, parent.Ref.ArtifactID, parent.Ref.SHA256,
		consumer.Task, consumer.Attempt, consumer.Fence,
		parent.Ref.ArtifactID, projected.Image.ID, projected.Image.SHA256,
		mask.Artifact.ID, mask.Artifact.SHA256, s.Bucket).Scan(
		&out.Root.ArtifactID, &out.Root.SHA256,
		&out.Params.ArtifactID, &out.Params.SHA256,
		&out.Report.ArtifactID, &out.Report.SHA256,
		&bucket, &out.Mask.Key, &out.Mask.VersionID,
		&out.Mask.SHA256, &out.Mask.Bytes, &out.Mask.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrInvalid
	}
	if err != nil {
		return zero, err
	}
	if bucket != s.Bucket || !validImportRef(out.Root) ||
		!validImportRef(out.Params) || !validImportRef(out.Report) ||
		out.Root.ArtifactID == out.Params.ArtifactID ||
		out.Root.ArtifactID == out.Report.ArtifactID ||
		out.Params.ArtifactID == out.Report.ArtifactID ||
		out.Mask.Key == "" || out.Mask.VersionID == "" ||
		out.Mask.VersionID == "null" || out.Mask.SHA256 != mask.Artifact.SHA256 ||
		out.Mask.Bytes < 1 || !exportArrayMediaType(out.Mask.ContentType) {
		return zero, v2input.ErrInvalid
	}
	final, err := s.LookupExportParamsOperation(ctx, parent, body)
	if err != nil || final != consumer {
		return zero, v2input.ErrInvalid
	}
	currentParent, err := s.LookupExportDocument(ctx, parent)
	if err != nil || currentParent != parentPin {
		return zero, v2input.ErrInvalid
	}
	currentImage, err := s.LookupExportDocument(ctx, image.Entry)
	if err != nil || currentImage != imagePin {
		return zero, v2input.ErrInvalid
	}
	return out, ctx.Err()
}

var _ ExportBodyPersonOriginSource = SQLSchedulerExportInputCatalog{}

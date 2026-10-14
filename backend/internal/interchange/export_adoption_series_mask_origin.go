package interchange

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func (s SQLSchedulerExportInputCatalog) LookupExportSeriesMaskOrigin(ctx context.Context,
	measure ExportSeriesMeasureOrigin, stat v2input.Entry, raw []byte,
	image ExportClosureNode, ref v2input.Ref) (ExportSeriesMaskOrigin, error) {
	var zero, origin ExportSeriesMaskOrigin
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || !validImportRef(ref) {
		return zero, v2input.ErrInvalid
	}
	refs, err := exportSeriesMeasureInputRefs(measure, raw, image)
	if err != nil {
		return zero, err
	}
	inputs, err := canonical.Marshal(refs)
	if err != nil {
		return zero, err
	}
	mask, err := s.LookupExportSeriesMeasureMask(ctx, measure, stat, raw, image, ref)
	if err != nil {
		return zero, err
	}
	origin.Mask = mask
	var rootBucket, paramsBucket, reportBucket string
	err = s.Claims.DB.QueryRow(ctx, `SELECT producer_task,producer_operation,producer_version,
 producer_attempt,producer_fence,
 root_id,root_sha,root_bucket,root_key,root_version,root_bytes,root_type,
 params_id,params_sha,params_bucket,params_key,params_version,params_bytes,params_type,
 report_id,report_sha,report_bucket,report_key,report_version,report_bytes,report_type
 FROM photo.v30_export_adoption_series_mask_origin($1,$2,$3,$4,$5,$6,$7,$8,$9,
 $10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21::jsonb,$22,$23,$24,$25)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, measure.Task, measure.Attempt, measure.Fence,
		stat.Ref.ArtifactID, stat.Ref.SHA256,
		measure.Params.ArtifactID, measure.Params.SHA256, s.Bucket,
		measure.Object.Key, measure.Object.VersionID, measure.Object.Bytes, inputs,
		image.Entry.Ref.ArtifactID, image.Entry.Ref.SHA256,
		ref.ArtifactID, ref.SHA256).Scan(
		&origin.Task, &origin.Operation, &origin.Version, &origin.Attempt, &origin.Fence,
		&origin.Root.Ref.ArtifactID, &origin.Root.Ref.SHA256, &rootBucket,
		&origin.Root.Object.Key, &origin.Root.Object.VersionID,
		&origin.Root.Object.Bytes, &origin.Root.Object.ContentType,
		&origin.Params.Ref.ArtifactID, &origin.Params.Ref.SHA256, &paramsBucket,
		&origin.Params.Object.Key, &origin.Params.Object.VersionID,
		&origin.Params.Object.Bytes, &origin.Params.Object.ContentType,
		&origin.Report.Ref.ArtifactID, &origin.Report.Ref.SHA256, &reportBucket,
		&origin.Report.Object.Key, &origin.Report.Object.VersionID,
		&origin.Report.Object.Bytes, &origin.Report.Object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrInvalid
	}
	if err != nil {
		return zero, err
	}
	if origin.Task == uuid.Nil || origin.Attempt < 1 || origin.Fence < 1 ||
		origin.Version != "1" || (origin.Operation != "mask.person" && origin.Operation != "mask.prompt") ||
		rootBucket != s.Bucket || paramsBucket != s.Bucket || reportBucket != s.Bucket {
		return zero, v2input.ErrInvalid
	}
	seen := map[uuid.UUID]bool{ref.ArtifactID: true}
	for _, doc := range []*ExportSeriesMaskDocument{&origin.Root, &origin.Params, &origin.Report} {
		doc.Object.SHA256 = doc.Ref.SHA256
		if !validImportRef(doc.Ref) || seen[doc.Ref.ArtifactID] ||
			doc.Object.Key == "" || doc.Object.VersionID == "" ||
			doc.Object.VersionID == "null" || doc.Object.Bytes < 2 ||
			doc.Object.Bytes > 1<<20 || doc.Object.ContentType != "application/json" {
			return zero, v2input.ErrInvalid
		}
		seen[doc.Ref.ArtifactID] = true
	}
	current, err := s.LookupExportSeriesMeasureMask(ctx, measure, stat, raw, image, ref)
	if err != nil {
		return zero, err
	}
	if current != mask {
		return zero, v2input.ErrInvalid
	}
	return origin, ctx.Err()
}

var _ ExportSeriesMaskOriginSource = SQLSchedulerExportInputCatalog{}

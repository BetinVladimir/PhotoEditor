package interchange

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

func (s SQLSchedulerExportInputCatalog) LookupExportSeriesMeasureMask(ctx context.Context,
	origin ExportSeriesMeasureOrigin, stat v2input.Entry, raw []byte,
	image ExportClosureNode, maskRef v2input.Ref) (objectstore.Object, error) {
	var zero, object objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || !validImportRef(maskRef) {
		return zero, v2input.ErrInvalid
	}
	refs, err := exportSeriesMeasureInputRefs(origin, raw, image)
	if err != nil {
		return zero, err
	}
	inputs, err := canonical.Marshal(refs)
	if err != nil {
		return zero, err
	}
	var params struct {
		Regions []struct {
			Mask json.RawMessage `json:"mask"`
		} `json:"regions"`
	}
	if json.Unmarshal(raw, &params) != nil {
		return zero, v2input.ErrInvalid
	}
	found := false
	for _, region := range params.Regions {
		mask, err := review.ParseMaskRef(region.Mask)
		if err != nil {
			return zero, err
		}
		if mask.Artifact.ID == maskRef.ArtifactID && mask.Artifact.SHA256 == maskRef.SHA256 {
			found = true
		}
	}
	if !found {
		return zero, v2input.ErrInvalid
	}
	if err := s.QualifyExportSeriesMeasureInputs(ctx, origin, stat, raw, image); err != nil {
		return zero, err
	}
	var bucket string
	err = s.Claims.DB.QueryRow(ctx, `SELECT bucket,object_key,object_version,sha256,bytes,media_type
 FROM photo.v30_export_adoption_series_mask($1,$2,$3,$4,$5,$6,$7,$8,$9,
 $10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21::jsonb,$22,$23,$24,$25)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, origin.Task, origin.Attempt, origin.Fence,
		stat.Ref.ArtifactID, stat.Ref.SHA256,
		origin.Params.ArtifactID, origin.Params.SHA256, s.Bucket,
		origin.Object.Key, origin.Object.VersionID, origin.Object.Bytes, inputs,
		image.Entry.Ref.ArtifactID, image.Entry.Ref.SHA256,
		maskRef.ArtifactID, maskRef.SHA256).Scan(&bucket, &object.Key,
		&object.VersionID, &object.SHA256, &object.Bytes, &object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrInvalid
	}
	if err != nil {
		return zero, err
	}
	if bucket != s.Bucket || object.Key == "" || object.VersionID == "" ||
		object.VersionID == "null" || object.SHA256 != maskRef.SHA256 ||
		object.Bytes < 1 || object.Bytes > 128<<30 ||
		(object.ContentType != "application/octet-stream" &&
			object.ContentType != "application/x-npy") {
		return zero, v2input.ErrInvalid
	}
	if err := s.QualifyExportSeriesMeasureInputs(ctx, origin, stat, raw, image); err != nil {
		return zero, err
	}
	return object, ctx.Err()
}

var _ ExportSeriesMeasureMaskSource = SQLSchedulerExportInputCatalog{}

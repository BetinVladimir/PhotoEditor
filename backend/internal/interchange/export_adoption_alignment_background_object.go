package interchange

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func (s SQLSchedulerExportInputCatalog) lookupExportAlignmentBackgroundObject(ctx context.Context,
	origin ExportBodyDependencyOrigin, parent v2input.Entry, raw []byte,
	fit ExportClosureNode, alignment v2input.Entry, params []byte,
	image ExportClosureNode) (objectstore.Object, error) {
	var zero, out objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil {
		return zero, v2input.ErrInvalid
	}
	projected, err := exportAlignmentBackgroundProjection(origin, parent, raw,
		fit, alignment, params, image)
	if err != nil {
		return zero, err
	}
	fit = ExportClosureNode{Entry: fit.Entry, Body: append([]byte(nil), fit.Body...)}
	body := append([]byte(nil), raw...)
	current, err := s.LookupExportBodyFitAlignmentOrigin(ctx, parent, body, fit, alignment)
	if err != nil {
		return zero, err
	}
	if current != origin {
		return zero, v2input.ErrInvalid
	}
	var bucket string
	err = s.Claims.DB.QueryRow(ctx, `SELECT bucket,object_key,object_version,sha256,bytes,media_type
 FROM photo.v30_export_adoption_alignment_background_object(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, alignment.Ref.ArtifactID, alignment.Ref.SHA256,
		origin.Producer.Task, origin.Producer.Attempt, origin.Producer.Fence,
		origin.Params.ArtifactID, projected.Artifact.ID, projected.Artifact.SHA256,
		s.Bucket).Scan(&bucket, &out.Key, &out.VersionID,
		&out.SHA256, &out.Bytes, &out.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrInvalid
	}
	if err != nil {
		return zero, err
	}
	if bucket != s.Bucket || out.Key == "" || out.VersionID == "" ||
		out.VersionID == "null" || out.SHA256 != projected.Artifact.SHA256 ||
		out.Bytes < 1 || out.Bytes > 800065536 ||
		(out.ContentType != "application/octet-stream" &&
			out.ContentType != "application/x-npy") {
		return zero, v2input.ErrInvalid
	}
	final, err := s.LookupExportBodyFitAlignmentOrigin(ctx, parent, body, fit, alignment)
	if err != nil {
		return zero, err
	}
	if final != origin {
		return zero, v2input.ErrInvalid
	}
	return out, ctx.Err()
}

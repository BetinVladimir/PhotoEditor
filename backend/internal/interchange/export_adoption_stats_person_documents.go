package interchange

import (
	"context"

	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func (s SQLSchedulerExportInputCatalog) LookupExportStatsPersonDocument(ctx context.Context,
	origin ExportStatsPersonOrigin, entry v2input.Entry) (objectstore.Object, error) {
	var zero, object objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil ||
		!validImportRef(origin.Root) || !validImportRef(origin.Params) ||
		!validImportRef(origin.Report) || !validImportRef(entry.Ref) ||
		origin.Root.ArtifactID == origin.Params.ArtifactID ||
		origin.Root.ArtifactID == origin.Report.ArtifactID ||
		origin.Params.ArtifactID == origin.Report.ArtifactID ||
		origin.Mask.Key == "" || origin.Mask.VersionID == "" ||
		origin.Mask.VersionID == "null" || origin.Mask.Bytes < 1 ||
		!exportArrayMediaType(origin.Mask.ContentType) {
		return zero, v2input.ErrInvalid
	}
	var expected v2input.Ref
	var maximum int64
	switch entry.Contract {
	case "MaskRef":
		expected, maximum = origin.Root, 1<<20
	case "PersonParams":
		expected, maximum = origin.Params, 65536
	case "PersonReport":
		expected, maximum = origin.Report, 1<<20
	default:
		return zero, v2input.ErrInvalid
	}
	if entry.Ref != expected {
		return zero, v2input.ErrInvalid
	}
	current, err := s.Claims.CurrentExportAdoptionClaim(ctx, s.Claim)
	if err != nil {
		return zero, err
	}
	if !current {
		return zero, v2input.ErrInvalid
	}
	rows, err := s.Claims.DB.Query(ctx, `SELECT bucket,object_key,object_version,sha256,bytes,media_type
 FROM photo.v30_export_adoption_stats_person_document($1,$2,$3,$4,$5,$6,$7,$8,$9,
 $10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, origin.Root.ArtifactID, origin.Root.SHA256,
		origin.Params.ArtifactID, origin.Params.SHA256,
		origin.Report.ArtifactID, origin.Report.SHA256,
		origin.Mask.SHA256, origin.Mask.Key, origin.Mask.VersionID,
		origin.Mask.Bytes, origin.Mask.ContentType, s.Bucket,
		entry.Ref.ArtifactID, entry.Ref.SHA256, entry.Contract)
	if err != nil {
		return zero, err
	}
	defer rows.Close()
	var bucket string
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return zero, err
		}
		return zero, v2input.ErrInvalid
	}
	if err := rows.Scan(&bucket, &object.Key, &object.VersionID,
		&object.SHA256, &object.Bytes, &object.ContentType); err != nil {
		return zero, err
	}
	if rows.Next() {
		return zero, v2input.ErrInvalid
	}
	if err := rows.Err(); err != nil {
		return zero, err
	}
	if bucket != s.Bucket || object.Key == "" || object.VersionID == "" ||
		object.VersionID == "null" || object.SHA256 != entry.Ref.SHA256 ||
		object.Bytes < 2 || object.Bytes > maximum ||
		object.ContentType != "application/json" {
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

var _ ExportStatsPersonDocumentCatalog = SQLSchedulerExportInputCatalog{}

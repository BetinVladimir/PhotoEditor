package interchange

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// SQLSchedulerExportOutputCatalog is bound to one scheduler claim. Each lookup
// repeats the lease fence and resolves only a positional output of its exact
// canonical result. ExportResultReader performs the subsequent byte validation
// and version recheck; this catalog does not authorize adoption.
type SQLSchedulerExportOutputCatalog struct {
	Claims SQLExportAdoptionClaimStore
	Claim  ExportAdoptionClaim
	Bucket string
}

func (s SQLSchedulerExportOutputCatalog) LookupExportOutput(ctx context.Context, entry v2input.Entry) (objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || !validImportRef(entry.Ref) ||
		len(s.Bucket) < 3 || len(s.Bucket) > 63 {
		return zero, v2input.ErrInvalid
	}
	switch entry.Contract {
	case "InterchangeResult", "MappingReport", "MappingShard", "InterchangeArchive":
	default:
		return zero, v2input.ErrInvalid
	}
	current, err := s.Claims.CurrentExportAdoptionClaim(ctx, s.Claim)
	if err != nil {
		return zero, err
	}
	if !current {
		return zero, v2input.ErrInvalid
	}
	var object objectstore.Object
	var bucket string
	err = s.Claims.DB.QueryRow(ctx, `SELECT bucket,object_key,object_version,sha256,bytes,media_type FROM photo.v30_export_adoption_output($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task, s.Claim.Result,
		s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA, s.Claim.Token,
		entry.Ref.ArtifactID, entry.Ref.SHA256, entry.Contract).
		Scan(&bucket, &object.Key, &object.VersionID, &object.SHA256, &object.Bytes, &object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrInvalid
	}
	if err != nil {
		return zero, err
	}
	maximum := int64(8 << 20)
	media := "application/json"
	if entry.Contract == "InterchangeResult" {
		maximum = 64 << 10
	} else if entry.Contract == "InterchangeArchive" {
		maximum = 64 << 30
		media = "application/zip"
	}
	if bucket != s.Bucket || object.Key == "" || object.VersionID == "" ||
		object.VersionID == "null" || object.SHA256 != entry.Ref.SHA256 ||
		object.Bytes < 2 || object.Bytes > maximum || object.ContentType != media {
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

var _ ExportOutputCatalog = SQLSchedulerExportOutputCatalog{}

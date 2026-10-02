package workflow

import (
	"bytes"
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
)

type IngestCatalogRepository struct {
	DB         artifactjson.RowQuerier
	WorkICCSHA string
}

// AdoptImage atomically registers asset/version, settles upload quota and
// creates policy-checked membership/publication. Run completion is separate.
func (r IngestCatalogRepository) AdoptImage(ctx context.Context, tenant, run uuid.UUID, image CatalogImage) (bool, error) {
	if r.DB == nil || tenant == uuid.Nil || run == uuid.Nil || image.Ordinal < 0 || image.Ordinal > 9007199254740991 || image.DecodeTaskID == uuid.Nil || !validExpansionRef(image.Source) || !validExpansionRef(image.MetadataRef) || image.Metadata.Source != image.Source {
		return false, errors.New("invalid catalog image")
	}
	raw, err := canonical.Marshal(image.Image)
	if err != nil {
		return false, err
	}
	// The canonical worker manifest digest is also the immutable version hash.
	// Revalidate typed data at the DB boundary rather than trusting a mutable DTO.
	if _, err = ReadDecodedImage(ctx, bytes.NewReader(raw), image.Outputs, image.Metadata, r.WorkICCSHA); err != nil {
		return false, err
	}
	metadataRaw, err := canonical.Marshal(image.Metadata)
	if err != nil {
		return false, err
	}
	var ok bool
	err = r.DB.QueryRow(ctx, "SELECT photo.adopt_ingest_image($1,$2,$3,$4,$5,$6,$7)", tenant, run, image.Ordinal, image.Outputs.Manifest.ID, image.Outputs.Manifest.SHA256, raw, metadataRaw).Scan(&ok)
	return ok, err
}

// RejectFile settles a terminal file failure confirmed again by SQL. It does not
// delete staging or treat retry_wait as a terminal outcome.
func (r IngestCatalogRepository) RejectFile(ctx context.Context, tenant, run uuid.UUID, f IngestItemFailure) (bool, error) {
	if r.DB == nil || tenant == uuid.Nil || run == uuid.Nil || f.Ordinal < 0 || f.Ordinal > 9007199254740991 || !validExpansionRef(f.Source) || (f.Reason != "failed" && f.Reason != "cancelled") || (f.Stage != "asset.verify" && f.Stage != "asset.inspect" && f.Stage != "asset.decode") {
		return false, errors.New("invalid ingest rejection")
	}
	var ok bool
	err := r.DB.QueryRow(ctx, "SELECT photo.reject_ingest_upload($1,$2,$3,$4,$5,$6,$7)", tenant, run, f.Ordinal, f.Source.ID, f.Source.SHA256, f.Stage, f.Reason).Scan(&ok)
	return ok, err
}

// Finish returns an empty status while the durable per-file barrier is incomplete.
// The database atomically updates run status and emits one creator-scoped event.
func (r IngestCatalogRepository) Finish(ctx context.Context, tenant, run uuid.UUID) (string, error) {
	if r.DB == nil || tenant == uuid.Nil || run == uuid.Nil {
		return "", errors.New("invalid ingest completion")
	}
	var status *string
	if err := r.DB.QueryRow(ctx, "SELECT photo.finish_ingest_run($1,$2)", tenant, run).Scan(&status); err != nil {
		return "", err
	}
	if status == nil {
		return "", nil
	}
	switch *status {
	case "completed", "completed_with_errors", "failed":
		return *status, nil
	}
	return "", errors.New("invalid ingest completion status")
}

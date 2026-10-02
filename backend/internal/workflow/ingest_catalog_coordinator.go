package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"sort"
)

type IngestCatalogSource interface {
	Load(context.Context, uuid.UUID, IngestSpec) (IngestCatalogBatch, error)
}
type IngestOriginalPreparer interface {
	Prepare(context.Context, OriginalScope) (review.ArtifactRef, error)
}
type IngestCatalogWriter interface {
	AdoptImage(context.Context, uuid.UUID, uuid.UUID, CatalogImage) (bool, error)
	RejectFile(context.Context, uuid.UUID, uuid.UUID, IngestItemFailure) (bool, error)
	Finish(context.Context, uuid.UUID, uuid.UUID) (string, error)
}
type IngestCatalogCoordinator struct {
	Source    IngestCatalogSource
	Originals IngestOriginalPreparer
	Catalog   IngestCatalogWriter
}

// Advance processes a fully validated batch in snapshot order. Each file is an
// independent idempotent transaction; failure leaves earlier commits intact and
// never calls Finish. Source must attest canonical decode and JSON integrity.
func (c IngestCatalogCoordinator) Advance(ctx context.Context, tenant uuid.UUID, spec IngestSpec) (bool, error) {
	invalid := errors.New("invalid ingest catalog batch")
	if c.Source == nil || c.Originals == nil || c.Catalog == nil || tenant == uuid.Nil {
		return false, invalid
	}
	registry, err := LoadRegistry()
	if err != nil {
		return false, err
	}
	if _, err = CompileIngest(spec, registry); err != nil {
		return false, err
	}
	batch, err := c.Source.Load(ctx, tenant, spec)
	if err != nil {
		return false, err
	}
	if !batch.Ready {
		return false, nil
	}
	if len(batch.Images)+len(batch.Failures) != len(spec.Items) {
		return false, invalid
	}
	expected := map[int64]review.ArtifactRef{}
	for _, item := range spec.Items {
		expected[item.Ordinal] = item.Source
	}
	type entry struct {
		ordinal int64
		image   *CatalogImage
		failure *IngestItemFailure
	}
	entries := make([]entry, 0, len(spec.Items))
	seen := map[int64]bool{}
	for i := range batch.Images {
		img := &batch.Images[i]
		source, ok := expected[img.Ordinal]
		if !ok || seen[img.Ordinal] || source != img.Source || img.Metadata.Source != source || img.DecodeTaskID == uuid.Nil || !validExpansionRef(img.Outputs.Manifest) {
			return false, invalid
		}
		seen[img.Ordinal] = true
		entries = append(entries, entry{ordinal: img.Ordinal, image: img})
	}
	for i := range batch.Failures {
		f := &batch.Failures[i]
		source, ok := expected[f.Ordinal]
		if !ok || seen[f.Ordinal] || source != f.Source || (f.Stage != "asset.verify" && f.Stage != "asset.inspect" && f.Stage != "asset.decode") || (f.Reason != "failed" && f.Reason != "cancelled") {
			return false, invalid
		}
		seen[f.Ordinal] = true
		entries = append(entries, entry{ordinal: f.Ordinal, failure: f})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].ordinal < entries[j].ordinal })
	for _, item := range entries {
		if err = ctx.Err(); err != nil {
			return false, err
		}
		var done bool
		if item.image != nil {
			img := *item.image
			original, e := c.Originals.Prepare(ctx, OriginalScope{Tenant: tenant, Run: spec.RunID, Ordinal: img.Ordinal, Source: img.Source})
			if e != nil {
				return false, e
			}
			if !validExpansionRef(original) || original.SHA256 != img.Source.SHA256 || original.ID == img.Source.ID {
				return false, errors.New("invalid prepared original")
			}
			done, err = c.Catalog.AdoptImage(ctx, tenant, spec.RunID, img)
		} else {
			done, err = c.Catalog.RejectFile(ctx, tenant, spec.RunID, *item.failure)
		}
		if err != nil {
			return false, err
		}
		if !done {
			return false, errors.New("ingest item settlement conflict")
		}
	}
	if err = ctx.Err(); err != nil {
		return false, err
	}
	status, err := c.Catalog.Finish(ctx, tenant, spec.RunID)
	if err != nil {
		return false, err
	}
	switch status {
	case "":
		return false, nil
	case "completed", "completed_with_errors", "failed":
		return true, nil
	default:
		return false, errors.New("invalid ingest completion status")
	}
}

package interchange

import (
	"context"

	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// SchedulerExportResult is verified JSON result structure and versioned root
// evidence. It is not an adoption receipt: archive bytes and portable source
// closure have not yet been qualified or committed.
type SchedulerExportResult struct {
	Intent CreateIntent
	Frozen FrozenExportDocuments
	Root   objectstore.Object
	Result ValidatedExportResult
}

type SchedulerExportResultReader struct {
	Claims  SQLExportAdoptionClaimStore
	Objects ImportDocumentObjects
	Bucket  string
}

func (s SchedulerExportResultReader) Read(ctx context.Context, claim ExportAdoptionClaim) (SchedulerExportResult, error) {
	var zero SchedulerExportResult
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Claims.DB == nil || s.Objects == nil || len(s.Bucket) < 3 ||
		len(s.Bucket) > 63 || claim.validate() != nil {
		return zero, v2input.ErrInvalid
	}
	intent, frozen, err := s.Claims.FrozenExportAdoption(ctx, claim)
	if err != nil {
		return zero, err
	}
	root, pinned, err := s.Claims.RootExportAdoption(ctx, claim, s.Bucket)
	if err != nil {
		return zero, err
	}
	catalog := SQLSchedulerExportOutputCatalog{Claims: s.Claims, Claim: claim, Bucket: s.Bucket}
	current, err := catalog.LookupExportOutput(ctx, v2input.Entry{Ref: root, Contract: "InterchangeResult"})
	if err != nil {
		return zero, err
	}
	if current != pinned {
		return zero, v2input.ErrInvalid
	}
	result, err := (ExportResultReader{Catalog: catalog, Objects: s.Objects}).Read(ctx, intent, frozen, root)
	if err != nil {
		return zero, err
	}
	latestRoot, latestObject, err := s.Claims.RootExportAdoption(ctx, claim, s.Bucket)
	if err != nil {
		return zero, err
	}
	if latestRoot != root || latestObject != pinned || result.Root() != root {
		return zero, v2input.ErrInvalid
	}
	return SchedulerExportResult{Intent: intent, Frozen: frozen, Root: pinned, Result: result}, ctx.Err()
}

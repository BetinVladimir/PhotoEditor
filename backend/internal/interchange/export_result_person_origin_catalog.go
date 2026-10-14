package interchange

import (
	"context"

	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Original person producer queries already prove their own current output,
// source acceptance and model provenance. Result-phase entry and root gates
// are repeated around those queries; draft document authority is never used.
func (s SQLExportResultInputCatalog) LookupExportStatsPersonOrigin(ctx context.Context,
	entry v2input.Entry, paramsRef v2input.Ref, raw []byte, image review.WorkingImage,
) (ExportStatsPersonOrigin, error) {
	var zero ExportStatsPersonOrigin
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if entry.Contract != "Stats20" || !validImportRef(s.Root) ||
		!validImportRef(entry.Ref) || !validImportRef(paramsRef) {
		return zero, v2input.ErrInvalid
	}
	before, err := s.LookupExportDocument(ctx, entry)
	if err != nil {
		return zero, err
	}
	config := s.Export
	original := SQLExportDocumentCatalog{DB: config.DB, Tenant: config.Tenant,
		Who: config.Who, Request: config.Request, Key: config.Key,
		Bucket: config.Bucket, Frozen: config.Frozen}
	origin, err := original.LookupExportStatsPersonOrigin(ctx, entry, paramsRef,
		append([]byte(nil), raw...), image)
	if err != nil {
		return zero, err
	}
	after, err := s.LookupExportDocument(ctx, entry)
	if err != nil {
		return zero, err
	}
	if after != before {
		return zero, v2input.ErrInvalid
	}
	return origin, ctx.Err()
}

func (s SQLExportResultInputCatalog) LookupExportStatsPersonDocument(ctx context.Context,
	origin ExportStatsPersonOrigin, entry v2input.Entry,
) (objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !validImportRef(s.Root) {
		return zero, v2input.ErrInvalid
	}
	root := v2input.Entry{Ref: s.Root, Contract: "InterchangeResult"}
	before, err := s.Export.LookupExportOutput(ctx, root)
	if err != nil {
		return zero, err
	}
	config := s.Export
	original := SQLExportDocumentCatalog{DB: config.DB, Tenant: config.Tenant,
		Who: config.Who, Request: config.Request, Key: config.Key,
		Bucket: config.Bucket, Frozen: config.Frozen}
	object, err := original.LookupExportStatsPersonDocument(ctx, origin, entry)
	if err != nil {
		return zero, err
	}
	after, err := s.Export.LookupExportOutput(ctx, root)
	if err != nil {
		return zero, err
	}
	if after != before {
		return zero, v2input.ErrInvalid
	}
	return object, ctx.Err()
}

var _ ExportStatsPersonOriginSource = SQLExportResultInputCatalog{}
var _ ExportStatsPersonDocumentCatalog = SQLExportResultInputCatalog{}

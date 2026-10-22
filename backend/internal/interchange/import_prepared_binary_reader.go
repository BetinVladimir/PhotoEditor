package interchange

import (
	"context"
	"photoeditor/backend/internal/v2input"
)

// Prepared arrays require fenced SQL provenance, verified bytes and independent
// native shape/dtype/geometry authority. Opaque streaming alone is insufficient.
type ImportPreparedBinaryNodeResolver struct {
	Catalog   ImportDocumentCatalog
	Objects   ImportDocumentObjects
	Qualifier ExportBinaryQualifier
	MaxBytes  int64
}

func (r ImportPreparedBinaryNodeResolver) ResolveImportNode(ctx context.Context, entry v2input.Entry) (ImportClosureNode, error) {
	if err := ctx.Err(); err != nil {
		return ImportClosureNode{}, err
	}
	if (entry.Contract != "MaskPixels" && entry.Contract != "DenseWarpField" && entry.Contract != "AbsoluteLUTArray" && entry.Contract != "DefectLabelMap" && entry.Contract != "WorkingPixels" && entry.Contract != "WorkingAlpha") || !validImportRef(entry.Ref) || r.Catalog == nil || r.Objects == nil || r.Qualifier == nil || r.MaxBytes < 1 || r.MaxBytes > 128<<30 {
		return ImportClosureNode{}, v2input.ErrInvalid
	}
	initial, err := r.Catalog.LookupImportDocument(ctx, entry)
	if err != nil {
		return ImportClosureNode{}, err
	}
	node, err := resolveImportBinary(ctx, entry, r.Catalog, r.Objects, r.MaxBytes)
	if err != nil {
		return ImportClosureNode{}, err
	}
	current, err := r.Catalog.LookupImportDocument(ctx, entry)
	if err != nil {
		return ImportClosureNode{}, err
	}
	if current != initial {
		return ImportClosureNode{}, v2input.ErrInvalid
	}
	if err := r.Qualifier.QualifyExportBinary(ctx, entry, current); err != nil {
		return ImportClosureNode{}, err
	}
	if err := ctx.Err(); err != nil {
		return ImportClosureNode{}, err
	}
	after, err := r.Catalog.LookupImportDocument(ctx, entry)
	if err != nil {
		return ImportClosureNode{}, err
	}
	if after != current {
		return ImportClosureNode{}, v2input.ErrInvalid
	}
	return node, ctx.Err()
}

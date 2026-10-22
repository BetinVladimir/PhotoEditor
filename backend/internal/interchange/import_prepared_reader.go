package interchange

import (
	"bytes"
	"context"
	"io"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// ImportPreparedDocumentReader reads qualified immutable JSON bytes. Full
// schema/operator validation belongs to the caller, not the media type.
type ImportPreparedDocumentReader interface {
	ReadPreparedImportDocument(context.Context, v2input.Entry, int64) ([]byte, error)
}

type BoundedPreparedImportReader struct {
	Catalog ImportDocumentCatalog
	Objects ImportDocumentObjects
}

func (r BoundedPreparedImportReader) ReadPreparedImportDocument(ctx context.Context, entry v2input.Entry, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.Catalog == nil || r.Objects == nil || !validImportRef(entry.Ref) || limit < 2 || limit > 8<<20 {
		return nil, v2input.ErrInvalid
	}
	switch entry.Contract {
	case "EditingRecipeV3", "StyleV2", "CorrectionPlan", "Override", "LayerStack", "MaskRef", "Params", "Stats20", "ResolvedStyle", "DefectCandidates", "DefectReview", "DustCandidates", "DefectProposeReport":
	default:
		return nil, v2input.ErrInvalid
	}
	object, err := r.Catalog.LookupImportDocument(ctx, entry)
	if err != nil {
		return nil, err
	}
	if object.Key == "" || object.VersionID == "" || object.VersionID == "null" || object.SHA256 != entry.Ref.SHA256 || object.ContentType != "application/json" || object.Bytes < 2 || object.Bytes > limit {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	reader, err := r.Objects.Open(ctx, object)
	if err != nil {
		if reader != nil {
			_ = reader.Close()
		}
		return nil, err
	}
	if reader == nil {
		return nil, v2input.ErrInvalid
	}
	// Checking cancellation per Read also bounds work when object IO ignores ctx.
	stream := &importPublicationReader{ctx: ctx, source: reader}
	raw, readErr := io.ReadAll(io.LimitReader(stream, object.Bytes+1))
	closeErr := reader.Close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if int64(len(raw)) != object.Bytes || rawSHA(raw) != entry.Ref.SHA256 {
		return nil, v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(raw, normalized) || raw[0] != '{' {
		return nil, v2input.ErrInvalid
	}
	current, err := r.Catalog.LookupImportDocument(ctx, entry)
	if err != nil {
		return nil, err
	}
	if current != object {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return raw, nil
}

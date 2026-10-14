package interchange

import (
	"bytes"
	"context"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// ExportOriginalSchemaChildren projects typed edges from original, immutable
// editor documents. It is structural only: the surrounding native and scoped
// validators must still qualify schema, producer, source, model and consent.
// It never applies import remapping to an exported document.
type ExportOriginalSchemaChildren struct{}

func (ExportOriginalSchemaChildren) ExportDocumentChildren(ctx context.Context,
	entry v2input.Entry, raw []byte,
) ([]v2input.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validImportRef(entry.Ref) || len(raw) < 2 || len(raw) > 8<<20 ||
		rawSHA(raw) != entry.Ref.SHA256 {
		return nil, v2input.ErrInvalid
	}
	body := append([]byte(nil), raw...)
	normalized, err := canonical.Bytes(body)
	if err != nil || !bytes.Equal(normalized, body) {
		return nil, v2input.ErrInvalid
	}
	if entry.Contract == "StyleV2" {
		// Style backgrounds use an independently resolved portable asset ID.
		// A raw artifact reference inside the style has no installed role.
		refs, err := v2draft.OpaqueDocumentChildren(body, entry.Contract, entry.Ref)
		if err != nil || len(refs) != 0 {
			return nil, v2input.ErrInvalid
		}
		return nil, ctx.Err()
	}
	children, err := (ImportPreparedSchemaChildren{}).ImportDocumentChildren(ctx, entry, body)
	if err != nil {
		return nil, err
	}
	return append([]v2input.Entry(nil), children...), ctx.Err()
}

var _ ExportTypedDocumentValidator = ExportOriginalSchemaChildren{}

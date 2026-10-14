package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Uses accepted-version draft-document ownership for the single server Params
// root. Source children retain the original contract-scoped catalog path.
type detectorParamsCatalog struct {
	reader   SQLTransferSourceReader
	source   v2draft.ArtifactCatalog
	binding  v2input.SessionVersionBinding
	document uuid.UUID
	params   v2input.Ref
}

func (c detectorParamsCatalog) Lookup(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (objectstore.Object, error) {
	if err := ctx.Err(); err != nil {
		return objectstore.Object{}, err
	}
	if c.document == uuid.Nil || c.source == nil {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	if node.Contract == "Params" {
		if node.Ref != c.params || node.ParentRef != (v2input.Ref{}) {
			return objectstore.Object{}, v2input.ErrInvalid
		}
		// Existing function checks exact draft Document owner, accepted version,
		// current source, tenant/project/editor and immutable ready object metadata.
		return c.reader.defectVersionParamsObject(ctx, who, tenant, c.binding, c.document, c.params)
	}
	if node.Ref.ArtifactID == c.params.ArtifactID {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return c.source.Lookup(ctx, who, tenant, node)
}

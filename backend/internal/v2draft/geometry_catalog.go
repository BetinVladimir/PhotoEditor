package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// DocumentGeometry is supplied only after a scoped catalog has verified an
// adopted ImageResult and agreement between its image and masks. Lookup must
// still authorize each occurrence before this immutable metadata is consumed.
type DocumentGeometry struct {
	CanvasHash    string
	TransformHash string
}

type GeometryCatalog interface {
	Geometry(context.Context, auth.Identity, uuid.UUID, LayerRef) (DocumentGeometry, bool, error)
}

func (s ClosureResolver) geometry(ctx context.Context, who auth.Identity, tenant uuid.UUID, node LayerRef) (DocumentGeometry, bool, error) {
	provider, ok := s.Catalog.(GeometryCatalog)
	if !ok {
		return DocumentGeometry{}, false, nil
	}
	value, found, err := provider.Geometry(ctx, who, tenant, node)
	if err != nil {
		return DocumentGeometry{}, false, err
	}
	if found && (!isDocumentRef(v2input.Ref{ArtifactID: node.Ref.ArtifactID, SHA256: value.CanvasHash}) || !isDocumentRef(v2input.Ref{ArtifactID: node.Ref.ArtifactID, SHA256: value.TransformHash})) {
		return DocumentGeometry{}, false, v2input.ErrInvalid
	}
	return value, found, nil
}

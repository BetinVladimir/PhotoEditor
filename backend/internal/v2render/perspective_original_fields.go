package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Only inverse fields already present in the SHA-verified original source can
// cross the adopted-output boundary. The original parent/path is retained for
// SQL authorization; a copied output path must never grant input authority.
func perspectiveOriginalDenseFields(raw []byte, source v2input.Ref) (map[uuid.UUID]v2draft.LayerRef, error) {
	children, err := v2draft.DocumentChildren(raw, "WorkingImage", source)
	if err != nil {
		return nil, err
	}
	roles := map[uuid.UUID]v2draft.LayerRef{}
	fields := map[uuid.UUID]v2draft.LayerRef{}
	for _, child := range children {
		if child.Ref.ArtifactID == source.ArtifactID {
			return nil, v2input.ErrInvalid
		}
		if prior, ok := roles[child.Ref.ArtifactID]; ok && (prior.Ref != child.Ref || prior.Contract != child.Contract) {
			return nil, v2input.ErrInvalid
		}
		roles[child.Ref.ArtifactID] = child
		if child.Contract == "DenseWarpField" {
			fields[child.Ref.ArtifactID] = child
		}
	}
	return fields, nil
}

func resolvePerspectiveOriginalField(ctx context.Context, original v2draft.ArtifactCatalog, who auth.Identity, tenant uuid.UUID, field v2draft.LayerRef) (objectstore.Object, error) {
	if err := ctx.Err(); err != nil {
		return objectstore.Object{}, err
	}
	if original == nil || field.Contract != "DenseWarpField" || !validLayerArtifactRef(field.Ref) || !validLayerArtifactRef(field.ParentRef) {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	obj, err := original.Lookup(ctx, who, tenant, field)
	if err != nil {
		return objectstore.Object{}, err
	}
	media, limit, ok := perspectiveChildObjectLimit(field.Contract)
	if !ok || obj.ContentType != media || obj.Bytes < 1 || obj.Bytes > limit || obj.SHA256 != field.Ref.SHA256 || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return obj, nil
}

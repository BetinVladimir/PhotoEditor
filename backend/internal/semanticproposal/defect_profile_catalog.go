package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// defectProfileCatalog authorizes only original Params of an already canonical
// exact-source defect producer. It does not authorize new arbitrary task Params.
type defectProfileCatalog struct {
	reader  SQLTransferSourceReader
	binding v2input.SessionVersionBinding
	pins    DefectOriginPins
}

func (c defectProfileCatalog) Lookup(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (objectstore.Object, error) {
	if err := ctx.Err(); err != nil {
		return objectstore.Object{}, err
	}
	if node.Contract == "FaceLandmarks" {
		if node.Path != "" || node.ParentRef != (v2input.Ref{}) || (node.CanvasHash != "" && node.CanvasHash != c.binding.CanvasHash) || node.ExpectedBinding != nil || node.ExpectedLearning != nil || node.ExpectedModelLockSHA != "" || node.ExpectedBackground != nil || node.RenderFrame != nil {
			return objectstore.Object{}, v2input.ErrInvalid
		}
		objects, err := c.reader.facePreparationObjects(ctx, who, tenant, c.binding, node.Ref, "face.landmarks")
		if err != nil {
			return objectstore.Object{}, err
		}
		if objects[0].ref != node.Ref {
			return objectstore.Object{}, v2input.ErrInvalid
		}
		return objects[0].object, nil
	}
	if node.Contract == "Params" {
		if node.Ref != c.pins.Params || node.Path != "" || node.ParentRef != (v2input.Ref{}) || node.CanvasHash != "" || node.ExpectedBinding != nil || node.ExpectedLearning != nil || node.ExpectedModelLockSHA != "" || node.ExpectedBackground != nil || node.RenderFrame != nil {
			return objectstore.Object{}, v2input.ErrInvalid
		}
		objects, err := c.reader.defectEvidenceObjects(ctx, who, tenant, c.binding, c.pins)
		if err != nil {
			return objectstore.Object{}, err
		}
		if len(objects) == 0 || objects[0].ref != node.Ref {
			return objectstore.Object{}, v2input.ErrInvalid
		}
		return objects[0].object, nil
	}
	catalog := v2draft.SQLArtifactCatalog{DB: c.reader.DB, Bucket: c.reader.Bucket, ProjectID: c.reader.ProjectID, AssetVersionID: c.binding.AssetVersionID}
	return catalog.Lookup(ctx, who, tenant, node)
}

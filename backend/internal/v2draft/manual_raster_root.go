package v2draft

import (
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// ManualRasterVectors is compiled from verified original producer Params.
// It is not accepted as caller-provided mask metadata or producer authority.
type ManualRasterVectors struct {
	Ref      v2input.Ref
	Contract string
}

func manualRasterRootRefs(raw []byte, path string, parent v2input.Ref, origin *ManualRasterVectors) ([]LayerRef, error) {
	if origin == nil || !isDocumentRef(origin.Ref) || (origin.Contract != "Params" && origin.Contract != "ManualMask") {
		return nil, v2input.ErrInvalid
	}
	root, err := review.ParseManualRasterDocument(raw, review.ArtifactRef{ID: parent.ArtifactID, SHA256: parent.SHA256})
	if err != nil {
		return nil, v2input.ErrInvalid
	}
	vector := v2input.Ref{ArtifactID: root.Vectors.ID, SHA256: root.Vectors.SHA256}
	if vector != origin.Ref {
		return nil, v2input.ErrInvalid
	}
	canvas, err := canonical.Hash(root.Target.Canvas)
	if err != nil {
		return nil, err
	}
	child := func(ref review.ArtifactRef, contract, key string) LayerRef {
		return LayerRef{Ref: v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, Contract: contract, ParentRef: parent, Path: path + key}
	}
	image := child(root.Image, "WorkingImage", ".image")
	image.CanvasHash = canvas
	vectors := child(root.Vectors, origin.Contract, ".vectors")
	if origin.Contract == "ManualMask" {
		vectors.CanvasHash = canvas
	}
	return []LayerRef{image, vectors, child(root.Target.Artifact, "MaskPixels", ".target.artifact"), child(root.Protected.Artifact, "MaskPixels", ".protected.artifact"), child(root.TargetPNG, "Artifact", ".target_png"), child(root.ProtectedPNG, "Artifact", ".protected_png")}, nil
}

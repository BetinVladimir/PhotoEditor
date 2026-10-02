package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// readCompositionJSON reads only an authorized pinned canonical document.
func readCompositionJSON(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef, catalog v2draft.ArtifactCatalog, objects v2input.SessionAnalysisObjectReader, maxBytes int64) ([]byte, error) {
	ref := node.Ref
	if !validLayerArtifactRef(ref) {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	obj, err := catalog.Lookup(ctx, who, tenant, node)
	if err != nil {
		return nil, err
	}
	if obj.Bytes < 2 || obj.Bytes > maxBytes || obj.ContentType != "application/json" || obj.SHA256 != ref.SHA256 || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" {
		return nil, v2input.ErrInvalid
	}
	reader, err := objects.Open(ctx, obj)
	if err != nil {
		return nil, err
	}
	if reader == nil {
		return nil, v2input.ErrInvalid
	}
	raw, readErr := readAdoptedRoot(ctx, reader, obj.Bytes)
	closeErr := reader.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	sum := sha256.Sum256(raw)
	jcs, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(jcs, raw) || hex.EncodeToString(sum[:]) != ref.SHA256 {
		return nil, v2input.ErrInvalid
	}
	return raw, nil
}

// VerifyCompositionParents checks immutable parent documents before transport
// publication. It does not grant null-source lineage or register a task: the
// eventual SQL admission must prove exact original/current version provenance.
func VerifyCompositionParents(ctx context.Context, request CompositionPreparation, catalog v2draft.ArtifactCatalog, objects v2input.SessionAnalysisObjectReader) error {
	if catalog == nil || objects == nil || request.Tenant == uuid.Nil || request.Actor.UserID == uuid.Nil || request.Actor.SessionID == uuid.Nil || request.Base.AssetVersionID == uuid.Nil || !validLayerArtifactRef(request.Base.Source) || !validLayerArtifactRef(request.ProposalResult) {
		return v2input.ErrInvalid
	}
	read := func(ref v2input.Ref, contract string) ([]byte, error) {
		return readCompositionJSON(ctx, request.Actor, request.Tenant, v2draft.LayerRef{Ref: ref, Contract: contract, Path: "CompositionParent"}, catalog, objects, 8<<20)
	}
	raw, err := read(request.Base.Source, "WorkingImage")
	if err != nil {
		return err
	}
	source, err := review.ParseWorkingImage(raw)
	if err != nil {
		return v2input.ErrInvalid
	}
	canvas, err := canonical.Hash(source.Canvas)
	if err != nil || canvas != request.Base.CanvasHash || int64(source.Canvas.Width) != request.Width || int64(source.Canvas.Height) != request.Height || source.DType != "float32" {
		return v2input.ErrInvalid
	}
	if request.SourceResult != nil {
		raw, err := read(*request.SourceResult, "ImageResult")
		if err != nil {
			return err
		}
		parent, err := v2draft.ParseImageResult(raw, *request.SourceResult)
		if err != nil || parent.WorkingImage != request.Base.Source {
			return v2input.ErrInvalid
		}
	}
	raw, err = read(request.ProposalResult, "ImageResult")
	if err != nil {
		return err
	}
	proposalResult, err := v2draft.ParseImageResult(raw, request.ProposalResult)
	if err != nil {
		return err
	}
	raw, err = read(proposalResult.WorkingImage, "WorkingImage")
	if err != nil {
		return err
	}
	proposal, err := review.ParseWorkingImage(raw)
	if err != nil || proposal.DType != "float32" || proposal.Canvas != source.Canvas || proposal.Color != source.Color || !bytes.Equal(proposal.TransformChain, source.TransformChain) {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

// PrepareVerifiedComposition combines parent byte/geometry verification with
// complete scoped dependency expansion and frozen transport construction.
// Callers must still atomically admit and register the resulting graph task.
func PrepareVerifiedComposition(ctx context.Context, request CompositionPreparation, catalog v2draft.ArtifactCatalog, objects v2input.SessionAnalysisObjectReader, publisher v2input.Publisher) (PreparedComposition, error) {
	if err := ctx.Err(); err != nil {
		return PreparedComposition{}, err
	}
	if request.Task == uuid.Nil || publisher == nil || catalog == nil || objects == nil {
		return PreparedComposition{}, v2input.ErrInvalid
	}
	// Reject malformed compilation/frozen params before any object IO.
	compiled, err := CompileComposition(request.Layer, request.Base, request.SourceResult, request.ProposalResult, request.ProtectedMask, request.Width, request.Height)
	if err != nil {
		return PreparedComposition{}, err
	}
	if _, err := CompositionInputRoots(compiled, request.ParamsRef); err != nil {
		return PreparedComposition{}, err
	}
	if err := VerifyCompositionParents(ctx, request, catalog, objects); err != nil {
		return PreparedComposition{}, err
	}
	if err := VerifyCompositionMasks(ctx, request.Actor, request.Tenant, compiled, request.ParamsRef, catalog, objects); err != nil {
		return PreparedComposition{}, err
	}
	return PrepareComposition(ctx, request, v2draft.ClosureResolver{Catalog: catalog, Objects: objects}, publisher)
}

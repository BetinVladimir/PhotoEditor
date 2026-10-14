package v2input

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

// ResolveSemanticMaskPixels proves exact document pin, protected disposition
// and geometry. It does not establish mask origin/authority or read NPY bytes.
func ResolveSemanticMaskPixels(maskRef Ref, maskBody json.RawMessage, binding SessionVersionBinding, imageBody json.RawMessage, protected bool) (Ref, error) {
	if !validRef(maskRef) || len(maskBody) < 2 || len(maskBody) > 8*1024*1024 {
		return Ref{}, ErrInvalid
	}
	sum := sha256.Sum256(maskBody)
	if hex.EncodeToString(sum[:]) != maskRef.SHA256 {
		return Ref{}, ErrInvalid
	}
	if _, err := ResolveSemanticSourceMetadata(binding, imageBody); err != nil {
		return Ref{}, err
	}
	image, err := review.ParseWorkingImage(imageBody)
	if err != nil {
		return Ref{}, ErrInvalid
	}
	mask, err := review.ParseMaskRef(maskBody)
	if err != nil || mask.Protected != protected || mask.Canvas != image.Canvas {
		return Ref{}, ErrInvalid
	}
	maskChain, err := canonical.Hash(mask.TransformChain)
	if err != nil {
		return Ref{}, ErrInvalid
	}
	imageChain, err := canonical.Hash(image.TransformChain)
	if err != nil || maskChain != imageChain {
		return Ref{}, ErrInvalid
	}
	return Ref{mask.Artifact.ID, mask.Artifact.SHA256}, nil
}

type SemanticPinnedDocuments struct {
	Reference, Target                                json.RawMessage
	SelectedMask, SourceProtection, TargetProtection json.RawMessage
}

// CompileSemanticTransferTaskFromDocuments derives all mask and image children
// from pinned documents; callers still establish scoped snapshot/model authority.
func CompileSemanticTransferTaskFromDocuments(workflow uuid.UUID, ordinal int, params SemanticTransferParams, intent SemanticEditIntent,
	paramsRef Ref, dependencies SemanticTransferDependencies, documents SemanticPinnedDocuments, profile json.RawMessage) (FrozenSemanticTask, error) {
	var err error
	dependencies.SelectedPixels, err = ResolveSemanticMaskPixels(intent.ReferenceMask, documents.SelectedMask, params.Reference, documents.Reference, false)
	if err != nil {
		return FrozenSemanticTask{}, err
	}
	dependencies.SourceProtectionPixels, err = ResolveSemanticMaskPixels(dependencies.SourceProtection, documents.SourceProtection, params.Reference, documents.Reference, true)
	if err != nil {
		return FrozenSemanticTask{}, err
	}
	dependencies.TargetProtectionPixels, err = ResolveSemanticMaskPixels(dependencies.TargetProtection, documents.TargetProtection, params.Target, documents.Target, true)
	if err != nil {
		return FrozenSemanticTask{}, err
	}
	return CompileSemanticTransferTaskFromSources(workflow, ordinal, params, intent, paramsRef, dependencies, documents.Reference, documents.Target, profile)
}

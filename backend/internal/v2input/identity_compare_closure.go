package v2input

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
)

// IdentityCompareDocuments are exact bytes read from independently authorized,
// immutable source, mask, calibration and consent catalog entries. No caller
// may provide pixel-child refs separately from these documents.
type IdentityCompareDocuments struct {
	Before, After            json.RawMessage
	EffectiveMask, Protected json.RawMessage
	Calibration, Consents    json.RawMessage
}

func identityPinnedDocument(ref Ref, body json.RawMessage) bool {
	if !validRef(ref) || len(body) < 2 || len(body) > 8<<20 {
		return false
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != ref.SHA256 {
		return false
	}
	canonicalBody, err := canonical.Bytes(body)
	return err == nil && bytes.Equal(body, canonicalBody)
}

// CompileIdentityCompareClosure declares every document and array the V23
// adapter can read. SQL lineage, consent epochs, model qualification, resource
// admission and task registration remain independent live gates.
func CompileIdentityCompareClosure(ctx context.Context, workflow uuid.UUID, ordinal int,
	params IdentityCompareParams, paramsRef Ref, documents IdentityCompareDocuments) (CaptureClosure, error) {
	if err := ctx.Err(); err != nil {
		return CaptureClosure{}, err
	}
	_, digest, err := CompileIdentityCompareParams(params)
	if err != nil || !validRef(paramsRef) || paramsRef.SHA256 != digest ||
		!identityPinnedDocument(params.Before.Source, documents.Before) ||
		!identityPinnedDocument(params.After.Source, documents.After) ||
		!identityPinnedDocument(params.EffectiveMask, documents.EffectiveMask) ||
		!identityPinnedDocument(params.Protected, documents.Protected) ||
		!identityPinnedDocument(params.Calibration, documents.Calibration) ||
		!identityPinnedDocument(params.Consents, documents.Consents) {
		return CaptureClosure{}, ErrInvalid
	}
	before, err := review.ParseWorkingImage(documents.Before)
	if err != nil {
		return CaptureClosure{}, ErrInvalid
	}
	after, err := review.ParseWorkingImage(documents.After)
	if err != nil || before.Canvas != after.Canvas || before.Color != after.Color ||
		before.DType != "float32" || after.DType != "float32" {
		return CaptureClosure{}, ErrInvalid
	}
	leftChain, err := canonical.Bytes(before.TransformChain)
	if err != nil {
		return CaptureClosure{}, ErrInvalid
	}
	rightChain, err := canonical.Bytes(after.TransformChain)
	if err != nil || !bytes.Equal(leftChain, rightChain) {
		return CaptureClosure{}, ErrInvalid
	}
	beforeInputs, err := prepareSemanticFaceSourceInputs(ctx, params.Before, documents.Before)
	if err != nil {
		return CaptureClosure{}, err
	}
	afterInputs, err := prepareSemanticFaceSourceInputs(ctx, params.After, documents.After)
	if err != nil {
		return CaptureClosure{}, err
	}
	effectivePixels, err := ResolveSemanticMaskPixels(params.EffectiveMask, documents.EffectiveMask, params.After, documents.After, false)
	if err != nil {
		return CaptureClosure{}, err
	}
	protectedPixels, err := ResolveSemanticMaskPixels(params.Protected, documents.Protected, params.After, documents.After, true)
	if err != nil {
		return CaptureClosure{}, err
	}
	entries := append(append([]Entry{}, beforeInputs...), afterInputs...)
	entries = append(entries,
		Entry{Ref: params.EffectiveMask, Contract: "MaskRef"},
		Entry{Ref: effectivePixels, Contract: "NpyArray"},
		Entry{Ref: params.Protected, Contract: "MaskRef"},
		Entry{Ref: protectedPixels, Contract: "NpyArray"},
		Entry{Ref: params.Calibration, Contract: "Calibration"},
		Entry{Ref: params.Consents, Contract: "ConsentSnapshot"},
		Entry{Ref: paramsRef, Contract: "Params"})
	task, err := TaskID(workflow, "identity.compare", ordinal)
	if err != nil {
		return CaptureClosure{}, err
	}
	closure, err := compileCaptureInventory(task, "identity.compare", params, paramsRef, entries)
	if err != nil {
		return CaptureClosure{}, err
	}
	if err := ctx.Err(); err != nil {
		return CaptureClosure{}, err
	}
	return closure, nil
}

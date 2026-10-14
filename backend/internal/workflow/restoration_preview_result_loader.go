package workflow

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

type RestorationPreviewResultReader interface {
	Read(context.Context, uuid.UUID, uuid.UUID, string, string) ([]MultiPreviewOutput, error)
}

// LoadRestorationPreviewResult binds a physically read manifest and encoded
// object hash to exactly one side/variant of a reconstructed four-task plan.
// Image format, embedded ICC and metadata still require native decoding.
func LoadRestorationPreviewResult(ctx context.Context, results RestorationPreviewResultReader,
	objects ArtifactOpener, plan RestorationPreviewPlan, baseRaw, candidateRaw []byte,
	side, variant, bucket string) (RenderDerivativeResult, error) {
	var zero RenderDerivativeResult
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if results == nil || objects == nil || bucket == "" ||
		(side != "before" && side != "after") ||
		(variant != "preview1600" && variant != "preview2560") {
		return zero, objectstore.ErrInvalid
	}
	if err := validateRestorationPreviewIntent(plan); err != nil {
		return zero, err
	}
	rebuilt, err := CompileRestorationPreviewPlan(plan.source, plan.srgb, baseRaw, candidateRaw)
	if err != nil {
		return zero, err
	}
	index := 0
	if side == "after" {
		index = 2
	}
	if variant == "preview2560" {
		index++
	}
	expected := rebuilt.Documents()[index]
	owned := plan.Documents()[index]
	if expected.Side != owned.Side || expected.ID != owned.ID ||
		expected.ParamsSHA != owned.ParamsSHA || string(expected.Params) != string(owned.Params) {
		return zero, objectstore.ErrIntegrity
	}
	imageRaw := baseRaw
	if side == "after" {
		imageRaw = candidateRaw
	}
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil {
		return zero, err
	}
	pair, err := results.Read(ctx, plan.source.Tenant, plan.run, side, variant)
	if err != nil {
		return zero, err
	}
	if len(pair) == 0 {
		return zero, objectstore.ErrMissing
	}
	if err := validateRestorationPreviewOutputs(plan.run, side, variant, bucket, pair); err != nil {
		return zero, err
	}
	result := RenderDerivativeResult{Task: pair[0].Task, Result: pair[0].Result,
		Attempt: pair[0].Attempt, Fence: pair[0].Fence}
	for _, output := range pair {
		if output.Params != string(expected.Params) {
			return zero, objectstore.ErrIntegrity
		}
		ref := review.ArtifactRef{ID: output.Artifact, SHA256: output.Object.SHA256}
		if output.Role == "derivative" {
			result.DerivativeRef, result.Derivative = ref, output.Object
		} else {
			result.ManifestRef, result.ManifestObject = ref, output.Object
		}
	}
	result.ManifestBytes, err = readRenderDerivativeJSON(ctx, objects, result.ManifestObject)
	if err != nil {
		return zero, err
	}
	result.Manifest, err = ParseRenderDerivativeManifest(result.ManifestBytes,
		result.ManifestRef, expected.Inputs[0], result.DerivativeRef,
		image, variant, plan.srgb)
	if err != nil {
		return zero, err
	}
	if result.Manifest.MediaType != result.Derivative.ContentType {
		return zero, objectstore.ErrIntegrity
	}
	if err := verifyMultiPreviewEncodedHash(ctx, objects, result.Derivative); err != nil {
		return zero, err
	}
	fresh, err := results.Read(ctx, plan.source.Tenant, plan.run, side, variant)
	if err != nil {
		return zero, err
	}
	if len(fresh) != len(pair) {
		return zero, objectstore.ErrIntegrity
	}
	for i := range pair {
		if fresh[i] != pair[i] {
			return zero, objectstore.ErrIntegrity
		}
	}
	return result, nil
}

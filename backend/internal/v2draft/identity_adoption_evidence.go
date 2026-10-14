package v2draft

import (
	"context"

	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// PreparedIdentityAdoption is physical pre-commit evidence, not authority to
// publish. The adoption transaction must revalidate current task, source,
// consent, qualification and every object pin represented here.
type PreparedIdentityAdoption struct {
	Scope        IdentityResultScope
	Inputs       IdentityResultInputs
	Documents    IdentityPixelDocuments
	Consents     IdentityConsentInput
	Calibration  IdentityCalibrationInput
	Result       VerifiedIdentityResult
	Measurements masknpy.IdentityPixelMeasurements
	PixelObjects map[string]objectstore.Object
}

// PrepareIdentityAdoption runs the entire physical input/output verification
// sequence. Caller-supplied refs are limited to canonical Params and result
// root; all other refs are derived from exact registered documents.
func PrepareIdentityAdoption(ctx context.Context, scope IdentityResultScope,
	paramsRef v2input.Ref, inputCatalog IdentityResultInputCatalog,
	outputCatalog IdentityResultCatalog, objects v2input.SessionAnalysisObjectReader) (PreparedIdentityAdoption, error) {
	var zero PreparedIdentityAdoption
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if inputCatalog == nil || outputCatalog == nil || objects == nil {
		return zero, v2input.ErrInvalid
	}
	inputs, err := LoadIdentityResultInputs(ctx, scope, inputCatalog, objects, paramsRef)
	if err != nil {
		return zero, err
	}
	documents, err := LoadIdentityPixelDocuments(ctx, scope, inputCatalog, objects, inputs)
	if err != nil {
		return zero, err
	}
	consents, err := LoadIdentityConsentInput(ctx, scope, inputCatalog, objects, inputs)
	if err != nil {
		return zero, err
	}
	calibration, err := LoadIdentityCalibrationInput(ctx, scope, inputCatalog, objects, inputs)
	if err != nil {
		return zero, err
	}
	result, err := VerifyIdentityPublishedResult(ctx, scope, outputCatalog, objects,
		inputs.ParamsRaw, inputs.AfterRaw)
	if err != nil {
		return zero, err
	}
	var pixelObjects map[string]objectstore.Object
	measurements, err := verifyIdentityPublishedPixels(ctx, scope, inputs, documents,
		result, inputCatalog, outputCatalog, objects, &pixelObjects)
	if err != nil {
		return zero, err
	}
	for _, item := range []struct {
		role string
		ref  v2input.Ref
		pin  objectstore.Object
	}{
		{"params", inputs.ParamsRef, inputs.Objects[0]},
		{"after", inputs.AfterRef, inputs.Objects[1]},
		{"before", documents.BeforeRef, documents.Objects[0]},
		{"effective", documents.EffectiveRef, documents.Objects[2]},
		{"protected", documents.ProtectedRef, documents.Objects[3]},
		{"consents", consents.Ref, consents.Object},
		{"calibration", calibration.Ref, calibration.Object}} {
		current, err := inputCatalog.ResolveIdentityResultInput(ctx, scope, item.role, item.ref)
		if err != nil {
			return zero, err
		}
		if current != item.pin {
			return zero, v2input.ErrExpired
		}
	}
	for i, item := range []struct {
		role string
		ref  v2input.Ref
	}{
		{"report", result.Report}, {"mask", result.Mask}, {"pixels", result.Pixels}} {
		current, err := outputCatalog.ResolveIdentityResultObject(ctx, scope, item.role, item.ref)
		if err != nil {
			return zero, err
		}
		if current != result.Objects[i] {
			return zero, v2input.ErrExpired
		}
	}
	for role, pin := range pixelObjects {
		ref, ok := identityPixelPinRef(documents, role)
		if !ok {
			return zero, v2input.ErrInvalid
		}
		current, err := inputCatalog.ResolveIdentityResultInput(ctx, scope, role, ref)
		if err != nil {
			return zero, err
		}
		if current != pin {
			return zero, v2input.ErrExpired
		}
	}
	if len(pixelObjects) != 4+boolToInt(documents.BeforeAlpha != nil)+boolToInt(documents.AfterAlpha != nil) {
		return zero, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return PreparedIdentityAdoption{Scope: scope, Inputs: inputs,
		Documents: documents, Consents: consents, Calibration: calibration,
		Result: result, Measurements: measurements, PixelObjects: pixelObjects}, nil
}

func identityPixelPinRef(documents IdentityPixelDocuments, role string) (v2input.Ref, bool) {
	switch role {
	case "before_pixels":
		return documents.BeforePixels, true
	case "after_pixels":
		return documents.AfterPixels, true
	case "effective_pixels":
		return documents.EffectivePixels, true
	case "protected_pixels":
		return documents.ProtectedPixels, true
	case "before_alpha":
		if documents.BeforeAlpha != nil {
			return *documents.BeforeAlpha, true
		}
	case "after_alpha":
		if documents.AfterAlpha != nil {
			return *documents.AfterAlpha, true
		}
	}
	return v2input.Ref{}, false
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

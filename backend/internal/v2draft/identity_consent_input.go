package v2draft

import (
	"context"

	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type IdentityConsentInput struct {
	Ref    v2input.Ref
	Raw    []byte
	Object objectstore.Object
}

// LoadIdentityConsentInput binds the physical ConsentSnapshot to exact task
// Params. Its shape is checked here; live grants require a transaction gate.
func LoadIdentityConsentInput(ctx context.Context, scope IdentityResultScope,
	catalog IdentityResultInputCatalog, objects v2input.SessionAnalysisObjectReader,
	inputs IdentityResultInputs) (IdentityConsentInput, error) {
	var zero IdentityConsentInput
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if catalog == nil || objects == nil || !isDocumentRef(inputs.Params.Consents) ||
		!isDocumentRef(inputs.ParamsRef) {
		return zero, v2input.ErrInvalid
	}
	ref := inputs.Params.Consents
	object, err := catalog.ResolveIdentityResultInput(ctx, scope, "consents", ref)
	if err != nil {
		return zero, err
	}
	raw, err := readJSONArtifact(ctx, objects, object, ref)
	if err != nil {
		return zero, err
	}
	if err = v2input.ValidateSessionConsentSnapshot(raw); err != nil {
		return zero, err
	}
	current, err := catalog.ResolveIdentityResultInput(ctx, scope, "consents", ref)
	if err != nil {
		return zero, err
	}
	if current != object {
		return zero, v2input.ErrExpired
	}
	return IdentityConsentInput{Ref: ref, Raw: raw, Object: object}, nil
}

package v2draft

import (
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type IdentityResultInputs struct {
	Params    v2input.IdentityCompareParams
	ParamsRaw []byte
	AfterRaw  []byte
	ParamsRef v2input.Ref
	AfterRef  v2input.Ref
	Objects   [2]objectstore.Object
}

// LoadIdentityResultInputs never accepts an independently supplied after ref:
// it derives that ref from the exact task Params, reads both registered inputs,
// then re-resolves their object versions after IO.
func LoadIdentityResultInputs(ctx context.Context, scope IdentityResultScope,
	catalog IdentityResultInputCatalog, objects v2input.SessionAnalysisObjectReader,
	paramsRef v2input.Ref) (IdentityResultInputs, error) {
	var zero IdentityResultInputs
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if scope.Tenant == uuid.Nil || scope.Task == uuid.Nil || scope.Result == uuid.Nil ||
		!isDocumentRef(scope.Root) || !isDocumentRef(paramsRef) || catalog == nil || objects == nil {
		return zero, v2input.ErrInvalid
	}
	paramsObject, err := catalog.ResolveIdentityResultInput(ctx, scope, "params", paramsRef)
	if err != nil {
		return zero, err
	}
	paramsRaw, err := readJSONArtifact(ctx, objects, paramsObject, paramsRef)
	if err != nil {
		return zero, err
	}
	params, err := v2input.DecodeIdentityCompareParams(paramsRaw)
	if err != nil {
		return zero, err
	}
	afterRef := params.After.Source
	afterObject, err := catalog.ResolveIdentityResultInput(ctx, scope, "after", afterRef)
	if err != nil {
		return zero, err
	}
	afterRaw, err := readJSONArtifact(ctx, objects, afterObject, afterRef)
	if err != nil {
		return zero, err
	}
	after, err := review.ParseWorkingImage(afterRaw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	if after.DType != "float32" || after.Color.ProfileID != "work_rec2020_gamma24_v1" ||
		after.Canvas.Width*after.Canvas.Height > 200000000 {
		return zero, v2input.ErrInvalid
	}
	for _, item := range []struct {
		role    string
		ref     v2input.Ref
		initial objectstore.Object
	}{
		{"params", paramsRef, paramsObject}, {"after", afterRef, afterObject}} {
		current, err := catalog.ResolveIdentityResultInput(ctx, scope, item.role, item.ref)
		if err != nil {
			return zero, err
		}
		if current != item.initial {
			return zero, v2input.ErrExpired
		}
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return IdentityResultInputs{Params: params, ParamsRaw: paramsRaw, AfterRaw: afterRaw,
		ParamsRef: paramsRef, AfterRef: afterRef, Objects: [2]objectstore.Object{paramsObject, afterObject}}, nil
}

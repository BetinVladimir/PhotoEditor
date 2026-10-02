package v2input

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
)

type PublishedLensDocuments struct {
	Params   Ref
	Original Ref
}

// PublishLensDocuments runs after the authorized draft run and frozen document
// plan commit, before final task shell staging. Returned IDs replace provisional
// refs before CompileLensTask; no transport/dispatch is created here.
func PublishLensDocuments(ctx context.Context, documents DocumentPublisher, tenant, run uuid.UUID,
	plan LensDocumentPlan) (PublishedLensDocuments, error) {
	var result PublishedLensDocuments
	if documents == nil || tenant == uuid.Nil || run == uuid.Nil || plan.TaskID == uuid.Nil ||
		plan.Params.SchemaVersion != 1 || plan.Params.Base != plan.Original.Binding ||
		plan.Params.Base.AssetVersionID == uuid.Nil || !validRef(plan.Params.Base.Source) || !validRef(plan.Params.Profile) ||
		!validRef(plan.Original.Original) || !digestPattern.MatchString(plan.Params.Base.CanvasHash) ||
		!finiteRange(plan.Params.Strength, 0, 1) || !finiteRange(plan.Params.FocalMM, 1, 2000) || !finiteRange(plan.Params.Aperture, .5, 128) {
		return result, ErrInvalid
	}
	params, err := canonical.Marshal(plan.Params)
	if err != nil {
		return result, err
	}
	original, err := canonical.Marshal(plan.Original)
	if err != nil {
		return result, err
	}
	paramsSHA, err := canonical.Hash(plan.Params)
	if err != nil {
		return result, err
	}
	originalSHA, err := canonical.Hash(plan.Original)
	if err != nil {
		return result, err
	}
	if len(params) > 1048576 || len(original) > 1048576 || !bytes.Equal(params, []byte(plan.ParamsJCS)) ||
		!bytes.Equal(original, []byte(plan.OriginalJCS)) || paramsSHA != plan.ParamsSHA || originalSHA != plan.OriginalSHA {
		return result, ErrInvalid
	}
	publish := func(purpose string, body []byte, sha string) (Ref, error) {
		if ctx.Err() != nil {
			return Ref{}, ctx.Err()
		}
		value, err := documents.Publish(ctx, artifactjson.Scope{Tenant: tenant, Run: run, Owner: plan.TaskID, Purpose: purpose}, body)
		if err != nil {
			return Ref{}, err
		}
		ref := Ref{value.ID, value.SHA256}
		if !validRef(ref) || ref.SHA256 != sha {
			return Ref{}, ErrInvalid
		}
		return ref, nil
	}
	result.Original, err = publish("v25_lens_original_binding", original, originalSHA)
	if err != nil {
		return PublishedLensDocuments{}, err
	}
	result.Params, err = publish("v25_lens_params", params, paramsSHA)
	if err != nil {
		return PublishedLensDocuments{}, err
	}
	return result, nil
}

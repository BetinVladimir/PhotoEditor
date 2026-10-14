package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// MultiResultContent owns typed documents whose source and output NPY bytes were
// verified in a completed current-scope read. It is not photographic acceptance,
// an Apply approval, an installed-ICC assertion or a cached access grant.
type MultiResultContent struct {
	documents       MultiResultDocuments
	cataloguePins   []multiChildPin
	valid           bool
	tenant, project uuid.UUID
}

func (v MultiResultContent) Binding() (AdoptedMultiBinding, error) {
	if !v.valid {
		return AdoptedMultiBinding{}, v2input.ErrInvalid
	}
	return v.documents.Binding()
}
func (v MultiResultContent) Image() (review.WorkingImage, error) {
	if !v.valid {
		return review.WorkingImage{}, v2input.ErrInvalid
	}
	return v.documents.Image()
}
func (v MultiResultContent) Mask() (review.MaskRef, error) {
	if !v.valid {
		return review.MaskRef{}, v2input.ErrInvalid
	}
	return v.documents.Mask()
}
func (v MultiResultContent) Report() (MultiImageReport, error) {
	if !v.valid {
		return MultiImageReport{}, v2input.ErrInvalid
	}
	return v.documents.Report()
}

// LoadAdoptedMultiContent requires both document/source closure and output-array
// verification. Failure at either phase returns no partially verified content.
func LoadAdoptedMultiContent(ctx context.Context, source AdoptedMultiCatalogueSource, objects v2input.SessionAnalysisObjectReader, who auth.Identity, tenant, project, producer uuid.UUID, operation string, expected v2input.Ref, bucket string) (MultiResultContent, error) {
	documents, err := LoadAdoptedMultiDocuments(ctx, source, objects, who, tenant, project, producer, operation, expected, bucket)
	if err != nil {
		return MultiResultContent{}, err
	}
	var pins []multiChildPin
	if err := verifyAdoptedMultiOutputArrays(ctx, source, objects, who, tenant, project, producer, documents, bucket, func(current []multiChildPin) { pins = current }); err != nil {
		return MultiResultContent{}, err
	}
	if err := ctx.Err(); err != nil {
		return MultiResultContent{}, err
	}
	return MultiResultContent{documents: documents, cataloguePins: pins, valid: true, tenant: tenant, project: project}, nil
}

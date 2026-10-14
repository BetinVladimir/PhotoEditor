package v2learning

import (
	"context"
	"reflect"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PreparedPortraitDetailHoldout struct {
	Document FrozenPortraitDetailHoldoutEvidence
	Body     []byte
	SHA256   string
}

// PreparePortraitDetailHoldout creates publication material from a linked,
// SQL-selected producer. This does not publish or register any artifact.
func PreparePortraitDetailHoldout(ctx context.Context, start TrainingStart,
	candidate TrainingCandidate, render TrainingRenderCase,
	source PortraitDetailProducerCandidate,
	imageRaw, paramsRaw, resolvedRaw, protectedRaw []byte,
) (PreparedPortraitDetailHoldout, error) {
	invalid := func() (PreparedPortraitDetailHoldout, error) {
		return PreparedPortraitDetailHoldout{}, ErrInvalidTrainingDraft
	}
	if err := ctx.Err(); err != nil {
		return PreparedPortraitDetailHoldout{}, err
	}
	if start.TenantID == uuid.Nil ||
		!validPortraitDetailProducerCandidate(source, candidate, render) ||
		render.DefectEvidence != nil || render.DustEvidence != nil {
		return invalid()
	}
	masks, err := ValidatePortraitDetailProducerMaterial(source, render.Protected,
		imageRaw, paramsRaw, resolvedRaw, protectedRaw)
	if err != nil {
		return invalid()
	}
	if err := ctx.Err(); err != nil {
		return PreparedPortraitDetailHoldout{}, err
	}
	origin := PortraitDetailHoldoutOrigin{
		TenantID: start.TenantID, EventID: candidate.EventID,
		AssetVersionID: candidate.AssetVersionID,
		RunID:          source.RunID, TaskID: source.TaskID,
		ResultID: source.ResultID, AttemptID: source.AttemptID,
		Source: source.Source, Image: source.Image,
		Protection: render.Protected,
		Params:     source.Params, Resolved: source.Resolved,
		MaskArtifacts: masks,
	}
	frozen, body, digest, err := CompileFrozenPortraitDetailHoldoutEvidence(
		origin, masks)
	if err != nil {
		return invalid()
	}
	return PreparedPortraitDetailHoldout{Document: frozen,
		Body: body, SHA256: digest}, nil
}

// PreparePortraitDetailForCandidate holds the training snapshot transaction
// while loading exact producer JSON through separately scoped, versioned
// object reads. Authority is repeated after IO before evidence is returned.
func PreparePortraitDetailForCandidate(ctx context.Context, tx pgx.Tx,
	objects SQLPortraitDetailObjects, masks SQLPortraitDetailMaskObjects,
	start TrainingStart,
	candidate TrainingCandidate, render TrainingRenderCase,
	protectedRaw []byte) (PreparedPortraitDetailHoldout, error) {
	before, err := LoadPortraitDetailProducerCandidate(
		ctx, tx, start, candidate, render)
	if err != nil {
		return PreparedPortraitDetailHoldout{}, err
	}
	imageRaw, err := objects.LoadJSON(ctx, start, candidate.EventID,
		"image", before.Image)
	if err != nil {
		return PreparedPortraitDetailHoldout{}, err
	}
	paramsRaw, err := objects.LoadJSON(ctx, start, candidate.EventID,
		"params", before.Params)
	if err != nil {
		return PreparedPortraitDetailHoldout{}, err
	}
	resolvedRaw, err := objects.LoadJSON(ctx, start, candidate.EventID,
		"resolved", before.Resolved)
	if err != nil {
		return PreparedPortraitDetailHoldout{}, err
	}
	maskRefs, err := ValidatePortraitDetailProducerMaterial(before,
		render.Protected, imageRaw, paramsRaw, resolvedRaw, protectedRaw)
	if err != nil {
		return PreparedPortraitDetailHoldout{}, err
	}
	for _, ref := range maskRefs {
		if err := masks.Verify(ctx, start, candidate.EventID, ref); err != nil {
			return PreparedPortraitDetailHoldout{}, err
		}
	}
	after, err := LoadPortraitDetailProducerCandidate(
		ctx, tx, start, candidate, render)
	if err != nil {
		return PreparedPortraitDetailHoldout{}, err
	}
	if !reflect.DeepEqual(before, after) {
		return PreparedPortraitDetailHoldout{}, ErrTrainingDraftConflict
	}
	return PreparePortraitDetailHoldout(ctx, start, candidate, render,
		before, imageRaw, paramsRaw, resolvedRaw, protectedRaw)
}

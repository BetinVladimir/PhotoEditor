package v2learning

import (
	"context"
	"reflect"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// PrepareReviewedDustForCandidate rechecks the unique SQL source around
// version-pinned object IO. The caller supplies the independently authorized
// training protection document and must recheck source authority at publish.
func PrepareReviewedDustForCandidate(ctx context.Context, tx pgx.Tx,
	objects SQLReviewedDustObjects, start TrainingStart,
	candidate TrainingCandidate, trainingRunID uuid.UUID,
	render TrainingRenderCase, recipes CompiledTrainingRecipes,
	protectedRaw []byte) (PreparedReviewedDustHoldout, error) {
	before, err := LoadReviewedDustForCandidate(ctx, tx, start, candidate, render)
	if err != nil {
		return PreparedReviewedDustHoldout{}, err
	}
	imageRaw, err := objects.LoadJSON(ctx, start, candidate.EventID,
		"image", before.Image)
	if err != nil {
		return PreparedReviewedDustHoldout{}, err
	}
	stageRaw, err := objects.LoadJSON(ctx, start, candidate.EventID,
		"stage_image", before.StageImage)
	if err != nil {
		return PreparedReviewedDustHoldout{}, err
	}
	proposalRaw, err := objects.LoadJSON(ctx, start, candidate.EventID,
		"proposal", before.Proposal)
	if err != nil {
		return PreparedReviewedDustHoldout{}, err
	}
	paramsRaw, err := objects.LoadJSON(ctx, start, candidate.EventID,
		"params", before.Params)
	if err != nil {
		return PreparedReviewedDustHoldout{}, err
	}
	after, err := LoadReviewedDustForCandidate(ctx, tx, start, candidate, render)
	if err != nil {
		return PreparedReviewedDustHoldout{}, err
	}
	if !reflect.DeepEqual(before, after) {
		return PreparedReviewedDustHoldout{}, ErrTrainingDraftConflict
	}
	return PrepareReviewedDustHoldout(ctx, candidate, trainingRunID, render,
		recipes, before, imageRaw, stageRaw, paramsRaw, proposalRaw,
		protectedRaw)
}

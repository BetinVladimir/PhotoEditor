package review

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
)

// The selected image is distinct from the task's immutable produced candidate.
// Skipped reviews intentionally expose no selected image.
type NegativeResultSelection struct {
	SchemaVersion int          `json:"schema_version"`
	ReviewID      uuid.UUID    `json:"review_id"`
	DecisionHash  string       `json:"decision_hash"`
	Route         string       `json:"route"`
	Image         *ArtifactRef `json:"image"`
}

// RenderResultSelection has the same closed receipt shape but a distinct
// operation-specific compiler and authority path.
type RenderResultSelection = NegativeResultSelection

// RestorationResultSelection describes the human-selected V28 image without
// changing the immutable producer result. Its authority must be established
// separately against the decided review and the adopted restoration receipt.
type RestorationResultSelection = NegativeResultSelection

func CompileNegativeResultSelection(snapshot CandidateSnapshot, actor uuid.UUID, decision Decision, persisted CandidatePlan) (NegativeResultSelection, error) {
	return compileCandidateResultSelection(snapshot, "optical.negative", actor, decision, persisted)
}

func CompileRenderResultSelection(snapshot CandidateSnapshot, actor uuid.UUID, decision Decision, persisted CandidatePlan) (RenderResultSelection, error) {
	return compileCandidateResultSelection(snapshot, "render.compose", actor, decision, persisted)
}

func CompileRestorationResultSelection(snapshot CandidateSnapshot, actor uuid.UUID, decision Decision, persisted CandidatePlan) (RestorationResultSelection, error) {
	if !localPortraitKind(snapshot.Kind) && snapshot.Kind != "portrait.glare" && snapshot.Kind != "portrait.open_eyes" && snapshot.Kind != "image.reflection" && snapshot.Kind != "image.relight" {
		return RestorationResultSelection{}, domain.NewError("INPUT_INVALID", 422)
	}
	return compileCandidateResultSelection(snapshot, snapshot.Kind, actor, decision, persisted)
}

func compileCandidateResultSelection(snapshot CandidateSnapshot, kind string, actor uuid.UUID, decision Decision, persisted CandidatePlan) (NegativeResultSelection, error) {
	var zero NegativeResultSelection
	if snapshot.Kind != kind {
		return zero, domain.NewError("INPUT_INVALID", 422)
	}
	expected, err := PlanCandidate(snapshot, actor, decision)
	if err != nil {
		return zero, err
	}
	expectedSHA, err := canonical.Hash(expected)
	if err != nil {
		return zero, err
	}
	actualSHA, err := canonical.Hash(persisted)
	if err != nil || expectedSHA != actualSHA {
		return zero, domain.NewError("STALE_CANDIDATE_REVIEW", 409)
	}
	result := NegativeResultSelection{SchemaVersion: 1, ReviewID: snapshot.ReviewID, DecisionHash: expected.DecisionHash, Route: expected.Route}
	switch expected.Route {
	case "candidate":
		image := snapshot.Candidate
		result.Image = &image
	case "base":
		image := snapshot.Base
		result.Image = &image
	case "skip":
	default:
		return zero, domain.NewError("INPUT_INVALID", 422)
	}
	return result, nil
}

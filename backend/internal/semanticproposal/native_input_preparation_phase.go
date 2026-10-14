package semanticproposal

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type nativeInputPreparationPhase string

const (
	nativePreparationBlocked   nativeInputPreparationPhase = "blocked"
	nativePreparationWaiting   nativeInputPreparationPhase = "waiting"
	nativePreparationSucceeded nativeInputPreparationPhase = "succeeded"
	nativePreparationFailed    nativeInputPreparationPhase = "failed"
)

// Interprets an original database observation only. Succeeded is not physical
// result authority; every continuation must still run complete result readers.
func nativeInputStagePreparationPhase(state nativeInputSavedStageState) (nativeInputPreparationPhase, error) {
	if state.task == uuid.Nil {
		return "", v2input.ErrInvalid
	}
	switch state.runState {
	case "draft", "queued", "running", "waiting_review", "completed", "completed_with_errors", "cancelling", "cancelled", "failed":
	default:
		return "", v2input.ErrInvalid
	}
	switch state.taskState {
	case "blocked", "ready", "queued", "running", "succeeded", "retry_wait", "failed", "cancelled", "skipped":
	default:
		return "", v2input.ErrInvalid
	}
	if state.runState == "failed" || state.runState == "cancelled" || state.runState == "cancelling" || state.taskState == "failed" || state.taskState == "cancelled" || state.taskState == "skipped" {
		return nativePreparationFailed, nil
	}
	if !state.sealed {
		if state.taskState != "blocked" || state.runState != "draft" {
			return "", v2input.ErrConflict
		}
		return nativePreparationBlocked, nil
	}
	if state.taskState == "succeeded" {
		return nativePreparationSucceeded, nil
	}
	if state.runState == "completed" || state.runState == "completed_with_errors" || state.runState == "waiting_review" {
		return "", v2input.ErrConflict
	}
	return nativePreparationWaiting, nil
}

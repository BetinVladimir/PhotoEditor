package semanticreview

import (
	"github.com/google/uuid"
	"math"
	"strconv"
)

type TargetPatch struct {
	TargetID   uuid.UUID
	State      string
	BatchID    uuid.UUID
	ChildRunID *uuid.UUID
}
type BatchTransition struct {
	Targets      []TargetPatch
	Remaining    int
	NextRevision string
	ParentState  string
}

// PlanBatch computes a complete patch after repository locks and live admission.
// Persistence must commit these patches, child Apply, receipt and revision together.
func PlanBatch(decision ApplyDecision, planSHA, revision, parentState string, states map[uuid.UUID]string, batchID uuid.UUID, childID *uuid.UUID) (BatchTransition, error) {
	var empty BatchTransition
	if parentState != "waiting_review" {
		return empty, ErrConflict
	}
	selected, err := ValidateSelection(decision, planSHA, revision, states)
	if err != nil {
		return empty, err
	}
	if batchID == uuid.Nil {
		return empty, ErrInvalid
	}
	current, _ := strconv.ParseInt(revision, 10, 64)
	if current == math.MaxInt64 {
		return empty, ErrConflict
	}
	approvals := 0
	remaining := 0
	for _, state := range states {
		if state == "proposed" {
			remaining++
		}
	}
	for _, item := range selected {
		if item.Decision == "approve" {
			approvals++
		}
	}
	if (approvals == 0 && childID != nil) || (approvals > 0 && (childID == nil || *childID == uuid.Nil)) {
		return empty, ErrInvalid
	}
	result := BatchTransition{Remaining: remaining - len(selected), NextRevision: strconv.FormatInt(current+1, 10), ParentState: "waiting_review"}
	for _, item := range selected {
		patch := TargetPatch{TargetID: item.TargetID, State: "rejected", BatchID: batchID}
		if item.Decision == "approve" {
			owned := *childID
			patch.State = "queued"
			patch.ChildRunID = &owned
		}
		result.Targets = append(result.Targets, patch)
	}
	return result, nil
}

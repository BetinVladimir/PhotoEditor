package v2learning

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"math"
	"sort"
	"time"

	"github.com/google/uuid"
)

var ErrInsufficientTrainingData = errors.New("INSUFFICIENT_DATA")
var ErrInvalidTrainingExample = errors.New("INVALID_TRAINING_EXAMPLE")

var targetLimits = [6]float64{1, 0.223144, 0.223144, 0.223144, 0.15, 0.15}

type EligibleTrainingExample struct {
	EventID          uuid.UUID
	SessionID        uuid.UUID
	SourceContentSHA string
	SourceLineageSHA string
	ApprovedAt       time.Time
	Features         [12]float64
	Target           [6]float64
}

type FrozenTrainingExample struct {
	Source EligibleTrainingExample
	Split  string
}

type TrainingSplit struct {
	Examples        []FrozenTrainingExample
	Excluded        []ExcludedTrainingCandidate
	TrainSessions   []uuid.UUID
	HoldoutSessions []uuid.UUID
}

func sessionOrder(id uuid.UUID) [32]byte {
	return sha256.Sum256([]byte("2026:" + id.String()))
}

func validTrainingExample(item EligibleTrainingExample) bool {
	if item.EventID == uuid.Nil || item.SessionID == uuid.Nil ||
		item.ApprovedAt.IsZero() || !validSHA(item.SourceContentSHA) ||
		!validSHA(item.SourceLineageSHA) {
		return false
	}
	for _, value := range item.Features {
		if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > 100 {
			return false
		}
	}
	for index, value := range item.Target {
		if math.IsNaN(value) || math.IsInf(value, 0) ||
			math.Abs(value) > targetLimits[index] {
			return false
		}
	}
	return true
}

// FreezeTrainingSplit mirrors the worker's freeze_examples algorithm. It
// removes duplicate/conflicting capture lineages before assigning whole
// sessions to train or holdout; it never adjusts the split to pass a threshold.
func FreezeTrainingSplit(candidates []EligibleTrainingExample) (TrainingSplit, error) {
	var out TrainingSplit
	if len(candidates) == 0 || len(candidates) > 5000 {
		return out, ErrInsufficientTrainingData
	}
	events := make(map[uuid.UUID]struct{}, len(candidates))
	sessionsByLineage := make(map[string]map[uuid.UUID]struct{})
	lineagesByContent := make(map[string]map[string]struct{})
	for _, item := range candidates {
		if !validTrainingExample(item) {
			return out, ErrInvalidTrainingExample
		}
		if _, duplicate := events[item.EventID]; duplicate {
			return out, ErrInvalidTrainingExample
		}
		events[item.EventID] = struct{}{}
		if sessionsByLineage[item.SourceLineageSHA] == nil {
			sessionsByLineage[item.SourceLineageSHA] = make(map[uuid.UUID]struct{})
		}
		sessionsByLineage[item.SourceLineageSHA][item.SessionID] = struct{}{}
		if lineagesByContent[item.SourceContentSHA] == nil {
			lineagesByContent[item.SourceContentSHA] = make(map[string]struct{})
		}
		lineagesByContent[item.SourceContentSHA][item.SourceLineageSHA] = struct{}{}
	}
	conflicted := make(map[string]struct{})
	for lineage, sessions := range sessionsByLineage {
		if len(sessions) > 1 {
			conflicted[lineage] = struct{}{}
		}
	}
	for _, lineages := range lineagesByContent {
		if len(lineages) > 1 {
			for lineage := range lineages {
				conflicted[lineage] = struct{}{}
			}
		}
	}
	latest := make(map[string]EligibleTrainingExample)
	for _, item := range candidates {
		if _, conflict := conflicted[item.SourceLineageSHA]; conflict {
			out.Excluded = append(out.Excluded, ExcludedTrainingCandidate{
				EventID: item.EventID, Reason: "CONFLICTING_LINEAGE"})
			continue
		}
		previous, exists := latest[item.SourceLineageSHA]
		if !exists || item.ApprovedAt.After(previous.ApprovedAt) ||
			item.ApprovedAt.Equal(previous.ApprovedAt) &&
				bytes.Compare(item.EventID[:], previous.EventID[:]) > 0 {
			if exists {
				out.Excluded = append(out.Excluded, ExcludedTrainingCandidate{
					EventID: previous.EventID, Reason: "DUPLICATE_LINEAGE"})
			}
			latest[item.SourceLineageSHA] = item
		} else {
			out.Excluded = append(out.Excluded, ExcludedTrainingCandidate{
				EventID: item.EventID, Reason: "DUPLICATE_LINEAGE"})
		}
	}
	kept := make([]EligibleTrainingExample, 0, len(latest))
	sessionSet := make(map[uuid.UUID]struct{})
	for _, item := range latest {
		kept = append(kept, item)
		sessionSet[item.SessionID] = struct{}{}
	}
	sessions := make([]uuid.UUID, 0, len(sessionSet))
	for id := range sessionSet {
		sessions = append(sessions, id)
	}
	sort.Slice(sessions, func(i, j int) bool {
		left, right := sessionOrder(sessions[i]), sessionOrder(sessions[j])
		if order := bytes.Compare(left[:], right[:]); order != 0 {
			return order < 0
		}
		return bytes.Compare(sessions[i][:], sessions[j][:]) < 0
	})
	if len(kept) < 40 || len(sessions) < 6 {
		return TrainingSplit{}, ErrInsufficientTrainingData
	}
	holdoutCount := int(math.Ceil(0.2 * float64(len(sessions))))
	if holdoutCount < 2 {
		holdoutCount = 2
	}
	if len(sessions)-holdoutCount < 4 {
		return TrainingSplit{}, ErrInsufficientTrainingData
	}
	out.HoldoutSessions = append(out.HoldoutSessions, sessions[:holdoutCount]...)
	out.TrainSessions = append(out.TrainSessions, sessions[holdoutCount:]...)
	position := make(map[uuid.UUID]int, len(sessions))
	for index, id := range sessions {
		position[id] = index
	}
	trainCount := 0
	for _, item := range kept {
		if position[item.SessionID] >= holdoutCount {
			trainCount++
		}
	}
	if trainCount < 20 || len(kept)-trainCount < 10 {
		return TrainingSplit{}, ErrInsufficientTrainingData
	}
	sort.Slice(kept, func(i, j int) bool {
		left, right := kept[i], kept[j]
		if position[left.SessionID] != position[right.SessionID] {
			return position[left.SessionID] < position[right.SessionID]
		}
		if !left.ApprovedAt.Equal(right.ApprovedAt) {
			return left.ApprovedAt.Before(right.ApprovedAt)
		}
		return bytes.Compare(left.EventID[:], right.EventID[:]) < 0
	})
	sort.Slice(out.Excluded, func(i, j int) bool {
		return bytes.Compare(out.Excluded[i].EventID[:],
			out.Excluded[j].EventID[:]) < 0
	})
	for _, item := range kept {
		split := "train"
		if position[item.SessionID] < holdoutCount {
			split = "holdout"
		}
		out.Examples = append(out.Examples, FrozenTrainingExample{
			Source: item, Split: split})
	}
	return out, nil
}

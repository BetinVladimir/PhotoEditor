package v2learning

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

func trainingCorpus() []EligibleTrainingExample {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	rows := make([]EligibleTrainingExample, 0, 60)
	for session := 0; session < 6; session++ {
		for index := 0; index < 10; index++ {
			number := session*10 + index + 1
			rows = append(rows, EligibleTrainingExample{
				EventID:          uuid.MustParse(fmt.Sprintf("00000000-0000-0000-0000-%012x", number)),
				SessionID:        uuid.MustParse(fmt.Sprintf("00000000-0000-0000-0000-%012x", session+1)),
				SourceContentSHA: fmt.Sprintf("%064x", number),
				SourceLineageSHA: fmt.Sprintf("%064x", number),
				ApprovedAt:       base.Add(time.Duration(number) * time.Minute),
				Features:         [12]float64{float64(number) / 100},
				Target:           [6]float64{0.1},
			})
		}
	}
	return rows
}

func TestFreezeTrainingSplitIsOrderIndependentAndSessionDisjoint(t *testing.T) {
	rows := trainingCorpus()
	first, err := FreezeTrainingSplit(rows)
	if err != nil {
		t.Fatal(err)
	}
	for i, j := 0, len(rows)-1; i < j; i, j = i+1, j-1 {
		rows[i], rows[j] = rows[j], rows[i]
	}
	second, err := FreezeTrainingSplit(rows)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("split changed with input order: %v", err)
	}
	if len(first.Examples) != 60 || len(first.HoldoutSessions) != 2 ||
		len(first.TrainSessions) != 4 {
		t.Fatalf("unexpected split sizes: %+v", first)
	}
	// Must match the Python worker's _session_order for UUID(int=1..6).
	for index, number := range []int{4, 2} {
		expected := uuid.MustParse(fmt.Sprintf("00000000-0000-0000-0000-%012x", number))
		if first.HoldoutSessions[index] != expected {
			t.Fatalf("Go/Python split mismatch: %v", first.HoldoutSessions)
		}
	}
	train := make(map[uuid.UUID]bool)
	for _, id := range first.TrainSessions {
		train[id] = true
	}
	for _, item := range first.Examples {
		if (item.Split == "train") != train[item.Source.SessionID] {
			t.Fatalf("cross-partition example: %+v", item)
		}
	}
}

func TestFreezeTrainingSplitRemovesConflictsBeforeSplit(t *testing.T) {
	rows := trainingCorpus()
	crossSession := rows[0]
	crossSession.EventID = uuid.New()
	crossSession.SessionID = rows[10].SessionID
	conflictingContent := rows[1]
	conflictingContent.EventID = uuid.New()
	conflictingContent.SourceLineageSHA = fmt.Sprintf("%064x", 1000)
	rows = append(rows, crossSession, conflictingContent)
	out, err := FreezeTrainingSplit(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Examples) != 58 || len(out.Excluded) != 4 {
		t.Fatalf("conflicts crossed split: %+v", out)
	}
	for _, excluded := range out.Excluded {
		if excluded.Reason != "CONFLICTING_LINEAGE" {
			t.Fatalf("wrong reason: %+v", excluded)
		}
	}
}

func TestFreezeTrainingSplitEnforcesPostDedupThreshold(t *testing.T) {
	rows := trainingCorpus()
	base, err := FreezeTrainingSplit(rows)
	if err != nil {
		t.Fatal(err)
	}
	held := make(map[uuid.UUID]int)
	for _, session := range base.HoldoutSessions {
		held[session] = 0
	}
	remaining := make([]EligibleTrainingExample, 0, len(rows))
	for _, item := range rows {
		if _, isHeld := held[item.SessionID]; isHeld && held[item.SessionID] < 6 {
			held[item.SessionID]++
			continue
		}
		remaining = append(remaining, item)
	}
	_, err = FreezeTrainingSplit(remaining)
	if !errors.Is(err, ErrInsufficientTrainingData) {
		t.Fatalf("accepted <10 holdout examples: %v", err)
	}
}

package v2learning

import (
	"bytes"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2draft"
)

func TestTrainingZeroBaselineIsRunScopedAndPredictsZero(t *testing.T) {
	run, profile := uuid.New(), uuid.New()
	first, err := BuildTrainingZeroBaseline(run, profile)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildTrainingZeroBaseline(run, profile)
	if err != nil || first.Ref != second.Ref || !bytes.Equal(first.Body, second.Body) {
		t.Fatal("zero baseline is not deterministic", err)
	}
	if !IsTrainingZeroBaseline(run, profile, first.Ref) ||
		IsTrainingZeroBaseline(uuid.New(), profile, first.Ref) ||
		IsTrainingZeroBaseline(run, uuid.New(), first.Ref) {
		t.Fatal("zero baseline escaped run or profile scope")
	}
	decoded, err := v2draft.DecodeCandidateProfile(first.Body)
	if err != nil || decoded != first.Profile {
		t.Fatal("invalid CandidateProfile zero document", err)
	}
	for _, value := range first.Profile.Intercept {
		if value != 0 {
			t.Fatal("nonzero intercept")
		}
	}
	for _, row := range first.Profile.Coefficients {
		for _, value := range row {
			if value != 0 {
				t.Fatal("nonzero coefficient")
			}
		}
	}
}

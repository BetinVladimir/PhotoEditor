package v2input

import (
	"math"
	"testing"

	"github.com/google/uuid"
)

func TestCaptureCoverageCountsUniqueQualifiedConfirmedGroups(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	group, scene := uuid.New(), uuid.New()
	shots := CaptureShotList{1, []CaptureShotRequirement{
		{group, "Group", []uuid.UUID{a, b}, 2, .7},
		{scene, "Scene", []uuid.UUID{}, 1, .7},
	}}
	quality, low := .7, .69
	version := uuid.New()
	frames := []CaptureConfirmedFrame{
		{version, []uuid.UUID{a, b}, &quality},
		{version, []uuid.UUID{b, a}, &quality}, // repeated join row
		{uuid.New(), []uuid.UUID{a}, &quality},
		{uuid.New(), []uuid.UUID{a, b}, &low},
		{uuid.New(), []uuid.UUID{a, b}, nil},
	}
	coverage, err := CompileCaptureCoverage(shots, frames)
	if err != nil || len(coverage) != 2 || coverage[0] != (CaptureCoverage{group, 1}) || coverage[1] != (CaptureCoverage{scene, 2}) {
		t.Fatalf("coverage=%v error=%v", coverage, err)
	}
	frames[1].PersonIDs = []uuid.UUID{a}
	if _, err := CompileCaptureCoverage(shots, frames); err == nil {
		t.Fatal("conflicting confirmation accepted")
	}
}

func TestCaptureCoverageRejectsInvalidEvidence(t *testing.T) {
	person := uuid.New()
	shots := CaptureShotList{1, []CaptureShotRequirement{{uuid.New(), "Portrait", []uuid.UUID{person}, 1, .5}}}
	for _, value := range []float64{math.NaN(), math.Inf(1), -.1, 1.1} {
		if _, err := CompileCaptureCoverage(shots, []CaptureConfirmedFrame{{uuid.New(), []uuid.UUID{person}, &value}}); err == nil {
			t.Fatalf("invalid quality accepted: %v", value)
		}
	}
	quality := .8
	if _, err := CompileCaptureCoverage(shots, []CaptureConfirmedFrame{{uuid.New(), []uuid.UUID{person, person}, &quality}}); err == nil {
		t.Fatal("duplicate person accepted")
	}
	coverage, err := CompileCaptureCoverage(shots, []CaptureConfirmedFrame{})
	if err != nil || len(coverage) != 1 || coverage[0].ConfirmedFrames != 0 {
		t.Fatalf("empty history: %v %v", coverage, err)
	}
}

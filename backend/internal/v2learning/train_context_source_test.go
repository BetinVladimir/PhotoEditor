package v2learning

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type contextModel struct{ value TrainingModelLock }

func (s contextModel) Load(context.Context, TrainingStart) (TrainingModelLock, error) {
	return s.value, nil
}

type contextCalibration struct{ value TrainingCalibration }

func (s contextCalibration) Load(context.Context, TrainingStart,
	string) (TrainingCalibration, error) {
	return s.value, nil
}

type contextBaseline struct{ value *TrainingActiveBaseline }

func (s contextBaseline) LoadActive(context.Context,
	TrainingStart) (*TrainingActiveBaseline, error) {
	return s.value, nil
}

func TestTrainingContextPinsQualificationAndBaseline(t *testing.T) {
	sha := func(letter string) string { return strings.Repeat(letter, 64) }
	calibration := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha("a")}
	sessions := make([]uuid.UUID, 6)
	for i := range sessions {
		sessions[i] = uuid.New()
	}
	start := TrainingStart{TenantID: uuid.New(), ProjectID: uuid.New(),
		ProfileID: uuid.New(), ActorID: uuid.New(), AuthSessionID: uuid.New(),
		Key: "training-test", Request: TrainCreate{ExpectedRevision: "1",
			SessionIDs: sessions, Policy: LearningPolicy{SchemaVersion: 1,
				Mode: "photographer", MinExamples: 40, MinSessions: 6,
				HoldoutFraction: .2, Seed: 2026,
				Calibration: calibration}}}
	inventory := TrainingInventory{ProjectID: start.ProjectID, Mode: "photographer"}
	source := TrainingContextSource{Model: contextModel{TrainingModelLock{
		ModelSHA: sha("b"), DatasetSHA: sha("c")}},
		Calibration: contextCalibration{TrainingCalibration{
			Ref: calibration, DatasetSHA: sha("c")}},
		Baseline: contextBaseline{}}
	got, err := source.Load(context.Background(), start, inventory, sha("d"))
	if err != nil {
		t.Fatal(err)
	}
	runID, _ := v2input.RunID(start.TenantID, start.ActorID, start.Key)
	if !IsTrainingZeroBaseline(runID, start.ProfileID, got.Baseline) ||
		got.ModelLockSHA != sha("b") || got.ICCLockSHA != sha("d") ||
		got.Calibration != calibration || got.PolicySHA == "" {
		t.Fatal("unfrozen qualification context", got)
	}
	source.Calibration = contextCalibration{TrainingCalibration{
		Ref: calibration, DatasetSHA: sha("e")}}
	if _, err := source.Load(context.Background(), start, inventory, sha("d")); err == nil {
		t.Fatal("accepted calibration from a different qualification dataset")
	}
	source.Calibration = contextCalibration{TrainingCalibration{
		Ref: calibration, DatasetSHA: sha("c")}}
	activeRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha("f")}
	source.Baseline = contextBaseline{&TrainingActiveBaseline{Ref: activeRef,
		TrainingSHA: sha("1"), Profile: v2draft.CandidateProfile{
			ProfileID: start.ProfileID, TrainingSHA: sha("1")}}}
	got, err = source.Load(context.Background(), start, inventory, sha("d"))
	if err != nil || got.Baseline != activeRef {
		t.Fatal("active baseline not pinned", got, err)
	}
}

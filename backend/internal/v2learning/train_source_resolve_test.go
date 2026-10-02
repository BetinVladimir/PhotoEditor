package v2learning

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type resolveInventory struct{ value TrainingInventory }

func (s resolveInventory) Load(context.Context,
	TrainingStart) (TrainingInventory, error) {
	return s.value, nil
}

type resolveBuilder struct {
	icc         string
	assembleErr error
}

func (s resolveBuilder) DiscoverICC(context.Context, TrainingStart,
	TrainingCandidate) (string, error) {
	return s.icc, nil
}

func (s resolveBuilder) Assemble(_ context.Context, _ TrainingStart,
	candidate TrainingCandidate, _ uuid.UUID, _, _ string) (
	TrainingCaseEvidence, error) {
	if s.assembleErr != nil {
		return TrainingCaseEvidence{}, s.assembleErr
	}
	return TrainingCaseEvidence{Source: EligibleTrainingExample{
		EventID: candidate.EventID, SessionID: candidate.SessionID}}, nil
}

type resolveFactory struct{ cases map[uuid.UUID]resolveBuilder }

func (s resolveFactory) ForCandidate(_ TrainingStart,
	candidate TrainingCandidate) (TrainingCaseBuilder, error) {
	item, okay := s.cases[candidate.EventID]
	if !okay {
		return nil, ErrInvalidTrainingDraft
	}
	return item, nil
}

func TestResolveExcludesOnlyExplicitUnsupportedCases(t *testing.T) {
	sha := func(letter string) string { return strings.Repeat(letter, 64) }
	sessions := make([]uuid.UUID, 6)
	for i := range sessions {
		sessions[i] = uuid.New()
	}
	calibration := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha("a")}
	start := TrainingStart{TenantID: uuid.New(), ActorID: uuid.New(),
		AuthSessionID: uuid.New(), ProjectID: uuid.New(),
		ProfileID: uuid.New(), Key: "resolve-training",
		Request: TrainCreate{ExpectedRevision: "1", SessionIDs: sessions,
			Policy: LearningPolicy{SchemaVersion: 1, Mode: "photographer",
				MinExamples: 40, MinSessions: 6, HoldoutFraction: .2,
				Seed: 2026, Calibration: calibration}}}
	cases := make(map[uuid.UUID]resolveBuilder)
	rows := make([]TrainingCandidate, 3)
	for i := range rows {
		approval := uuid.New()
		rows[i] = TrainingCandidate{EventID: uuid.New(),
			SessionID: sessions[i], ConsentLive: true, SourceReady: true,
			BaseReady: true, ApprovedReady: true, ApprovalID: &approval,
			ChangedPaths: []string{"/ev"}}
	}
	cases[rows[0].EventID] = resolveBuilder{icc: sha("b"),
		assembleErr: ErrInvalidTrainingExample}
	cases[rows[1].EventID] = resolveBuilder{icc: sha("c")}
	cases[rows[2].EventID] = resolveBuilder{icc: sha("d")}
	inventory := TrainingInventory{ProjectID: start.ProjectID,
		Mode: "photographer", Rows: rows}
	source := TrainingSourceResolver{Inventory: resolveInventory{inventory},
		Context: TrainingContextSource{
			Model: contextModel{TrainingModelLock{ModelSHA: sha("e"),
				DatasetSHA: sha("f")}},
			Calibration: contextCalibration{TrainingCalibration{
				Ref: calibration, DatasetSHA: sha("f")}},
			Baseline: contextBaseline{}}, Cases: resolveFactory{cases}}
	got, err := source.Resolve(context.Background(), start)
	if err != nil || len(got.Candidates) != 3 || len(got.Evidence) != 1 ||
		got.Evidence[0].Source.EventID != rows[1].EventID ||
		len(got.Excluded) != 2 || got.Excluded[0].EventID != rows[0].EventID ||
		got.Excluded[1].EventID != rows[2].EventID ||
		got.Context.ICCLockSHA != sha("c") ||
		got.ReservedBytes != 16*1048576 {
		t.Fatalf("resolve did not preserve exact inventory/exclusions: %+v %v",
			got, err)
	}
}

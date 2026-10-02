package v2sessions

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type replayAdmission struct {
	probe, admit int
	outcome      string
}

func (f *replayAdmission) Probe(_ context.Context,
	r v2input.SessionAnalysisBeginRequest) (v2input.SessionAnalysisReplayResult, error) {
	f.probe++
	outcome := f.outcome
	if outcome == "" {
		outcome = "replayed"
	}
	return v2input.SessionAnalysisReplayResult{Outcome: outcome,
		Run: r.Selection.Run}, nil
}

func (f *replayAdmission) Admit(_ context.Context,
	r v2input.SessionAnalysisBeginRequest) (v2input.JobTicket, error) {
	f.admit++
	if r.ReservedBytes != 1 || r.Selection.AuthSession == uuid.Nil ||
		r.Selection.ManualReferences == nil {
		return v2input.JobTicket{}, v2input.ErrInvalid
	}
	workflow, _ := v2input.WorkflowID(r.Selection.Run)
	return v2input.JobTicket{RunID: r.Selection.Run,
		WorkflowID: workflow, State: "queued"}, nil
}

func TestAnalyzeSealedReplaySkipsExpiredSelectionPreflight(t *testing.T) {
	admission := &replayAdmission{}
	svc := Service{DB: &pgxpool.Pool{}, Admission: admission}
	identity := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	request := AnalysisRequest{ExpectedRevision: "1",
		SelectionSnapshotID: uuid.New(),
		Policy: SessionPolicy{SchemaVersion: 1, TimeGapSeconds: 60,
			NeighborCount: 4, ProtectManualPicks: true,
			CameraOffsets: []CameraOffset{},
			Calibration: ArtifactRef{ArtifactID: uuid.New(),
				SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		ManualReferences: []ReferenceChoice{}}
	ticket, replayed, err := svc.Analyze(context.Background(), identity,
		uuid.New(), uuid.New(), "replay-key", request)
	if err != nil || !replayed || ticket.State != "queued" ||
		admission.probe != 1 || admission.admit != 1 {
		t.Fatalf("sealed replay: %+v %v replay=%t calls=%d/%d",
			ticket, err, replayed, admission.probe, admission.admit)
	}
}

func TestAnalyzePendingResumesWithoutSelectionPreflight(t *testing.T) {
	admission := &replayAdmission{outcome: "pending"}
	svc := Service{DB: &pgxpool.Pool{}, Admission: admission}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	request := AnalysisRequest{ExpectedRevision: "1", SelectionSnapshotID: uuid.New(),
		Policy: SessionPolicy{SchemaVersion: 1, TimeGapSeconds: 60,
			NeighborCount: 4, ProtectManualPicks: true,
			CameraOffsets: []CameraOffset{}, Calibration: ArtifactRef{
				ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		ManualReferences: []ReferenceChoice{}}
	ticket, replayed, err := svc.Analyze(context.Background(), who,
		uuid.New(), uuid.New(), "pending-key", request)
	if err != nil || replayed || ticket.State != "queued" ||
		admission.probe != 1 || admission.admit != 1 {
		t.Fatalf("pending retry: %+v %v replay=%t", ticket, err, replayed)
	}
}

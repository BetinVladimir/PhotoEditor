package v2sessions

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

type replayAdmission struct {
	probe, admit int
	outcome      string
	onProbe func()
	onAdmit func()
	ticket *v2input.JobTicket
}

func (f *replayAdmission) Probe(_ context.Context,
	r v2input.SessionAnalysisBeginRequest) (v2input.SessionAnalysisReplayResult, error) {
	f.probe++
	if f.onProbe != nil { f.onProbe() }
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
	if f.onAdmit != nil { f.onAdmit() }
	if f.ticket != nil { return *f.ticket, nil }
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


func analysisReplayRequest() AnalysisRequest {
	return AnalysisRequest{ExpectedRevision: "1", SelectionSnapshotID: uuid.New(),
		Policy: SessionPolicy{SchemaVersion: 1, TimeGapSeconds: 60, NeighborCount: 4,
			ProtectManualPicks: true, CameraOffsets: []CameraOffset{},
			Calibration: ArtifactRef{ArtifactID: uuid.New(), SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}},
		ManualReferences: []ReferenceChoice{}}
}

func TestAnalyzeRefusesUnboundAdmissionTicket(t *testing.T) {
	for _, outcome := range []string{"pending", "replayed"} {
		for _, field := range []string{"run", "workflow", "state"} {
			t.Run(outcome+"/"+field, func(t *testing.T) {
				who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
				tenantID := uuid.New()
				run, err := v2input.RunID(tenantID, who.UserID, "bound-key")
				if err != nil { t.Fatal(err) }
				workflow, err := v2input.WorkflowID(run)
				if err != nil { t.Fatal(err) }
				ticket := v2input.JobTicket{RunID: run, WorkflowID: workflow, State: "queued"}
				switch field { case "run": ticket.RunID=uuid.New(); case "workflow": ticket.WorkflowID=uuid.New(); case "state": ticket.State="completed" }
				admission := &replayAdmission{outcome: outcome, ticket: &ticket}
				svc := Service{DB: &pgxpool.Pool{}, Admission: admission}
				actual, _, err := svc.Analyze(context.Background(), who, tenantID, uuid.New(), "bound-key", analysisReplayRequest())
				if err == nil || actual.RunID != uuid.Nil { t.Fatal("unbound ticket returned", actual, err) }
			})
		}
	}
}

func TestAnalyzeCancellationFencesAdmissionStages(t *testing.T) {
	for _, stage := range []string{"before", "probe", "admit"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			admission := &replayAdmission{}
			switch stage { case "before": cancel(); case "probe": admission.onProbe=cancel; case "admit": admission.onAdmit=cancel }
			svc := Service{DB: &pgxpool.Pool{}, Admission: admission}
			actual, _, err := svc.Analyze(ctx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), "cancel-key", analysisReplayRequest())
			if !errors.Is(err, context.Canceled) || actual.RunID != uuid.Nil { t.Fatal("cancelled ticket exposed", actual, err) }
			if stage == "before" && admission.probe != 0 { t.Fatal("probe after cancellation") }
			if stage != "admit" && admission.admit != 0 { t.Fatal("admission after cancellation") }
		})
	}
}

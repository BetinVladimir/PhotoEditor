package v2input

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

type analysisBeginStub struct{ result SessionAnalysisBeginResult }

func (s analysisBeginStub) Begin(context.Context,
	SessionAnalysisBeginRequest) (SessionAnalysisBeginResult, error) {
	return s.result, nil
}

type analysisPendingStub struct{ calls *int }

func (s analysisPendingStub) AdmitPending(_ context.Context,
	draft SessionAnalysisDraft) (JobTicket, error) {
	*s.calls++
	if len(draft.Items) != 1 || !validRef(draft.Calibration) ||
		!digestPattern.MatchString(draft.SnapshotDigest) {
		return JobTicket{}, ErrInvalid
	}
	return JobTicket{RunID: draft.Run, WorkflowID: draft.Workflow, State: "queued"}, nil
}

type analysisProfilesStub struct{ calls *int }

func (s analysisProfilesStub) Compile(_ context.Context, _, _, _ uuid.UUID,
	_ SessionInputSnapshot) ([]json.RawMessage, error) {
	*s.calls++
	return []json.RawMessage{json.RawMessage(`{"backend":"cuda","renderer_version":"v2.2","resource_estimate":{"ram_bytes":2147483648,"vram_bytes":6442450944,"scratch_bytes":1073741824,"output_bytes":8388608,"pixel_count":0}}`)}, nil
}

type analysisTicketStub struct{ ticket JobTicket }

func (s analysisTicketStub) Ticket(context.Context, uuid.UUID, uuid.UUID,
	string, string) (JobTicket, error) {
	return s.ticket, nil
}

func TestSessionAnalysisCoordinatorSkipsPublicationOnReplay(t *testing.T) {
	tenant, actor, session, selection := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	key := "analysis-test"
	run, _ := RunID(tenant, actor, key)
	workflow, _ := WorkflowID(run)
	image := Ref{uuid.New(), strings.Repeat("a", 64)}
	snapshot, _, err := CompileSessionInputSnapshot(run, []SessionSnapshotItem{{
		Ordinal: 0, Binding: SessionVersionBinding{AssetVersionID: uuid.New(),
			Source: image, CanvasHash: strings.Repeat("b", 64)},
		WorkingImage: image, Recipe: Ref{uuid.New(), strings.Repeat("c", 64)}}})
	if err != nil {
		t.Fatal(err)
	}
	calibration := Ref{uuid.New(), strings.Repeat("d", 64)}
	policy, _ := canonical.Marshal(map[string]any{"calibration": calibration})
	request := SessionAnalysisBeginRequest{Selection: SessionAnalysisSelection{
		Tenant: tenant, Run: run, Session: session, Snapshot: selection,
		Actor: actor, AuthSession: uuid.New(), ExpectedRevision: 1,
		ManualReferences: json.RawMessage(`[]`)}, Policy: policy,
		ModelLockSHA: strings.Repeat("e", 64), Key: key, ReservedBytes: 1024}
	begin := SessionAnalysisBeginResult{Outcome: "created", Run: run,
		Workflow: workflow, RequestSHA: strings.Repeat("f", 64),
		PolicySHA: strings.Repeat("1", 64), Snapshot: snapshot,
		ModelLockSHA: strings.Repeat("e", 64),
		ContextBody:  []byte(`{}`), ContextSHA: strings.Repeat("2", 64)}
	ticket := JobTicket{RunID: run, WorkflowID: workflow, State: "queued"}
	profiles, pending := 0, 0
	coordinator := SessionAnalysisCoordinator{Beginner: analysisBeginStub{begin},
		Pending:  analysisPendingStub{&pending},
		Profiles: analysisProfilesStub{&profiles},
		Tickets:  analysisTicketStub{ticket}}
	if got, err := coordinator.Admit(context.Background(), request); err != nil ||
		got != ticket || profiles != 1 || pending != 1 {
		t.Fatalf("created admission: %+v %v profiles=%d pending=%d", got, err, profiles, pending)
	}
	begin.Outcome = "replayed"
	coordinator.Beginner = analysisBeginStub{begin}
	if got, err := coordinator.Admit(context.Background(), request); err != nil ||
		got != ticket || profiles != 1 || pending != 1 {
		t.Fatalf("replay: %+v %v profiles=%d pending=%d", got, err, profiles, pending)
	}
}

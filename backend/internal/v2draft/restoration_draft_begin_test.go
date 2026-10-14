package v2draft

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

func TestGlareDonorDraftBeginBindsServerIdentityAndReplayConfig(t *testing.T) {
	sha := strings.Repeat("a", 64)
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
	pin := v2portrait.RestorationQualificationPin{Operation: "portrait.glare",
		OperationVersion: "1", PolicyID: uuid.New(), QualificationID: uuid.New(),
		PolicySHA: sha, Report: ref(), Calibration: ref(), ModelLockSHA: sha,
		RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha,
		IssuerID: uuid.New(), Backend: "cpu"}
	base, donor := uuid.New(), uuid.New()
	tenant, actor := uuid.New(), uuid.New()
	run, err := v2input.RunID(tenant, actor, "glare-begin-1")
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := v2input.TaskID(workflow, "portrait.glare", 3)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := canonical.Marshal(map[string]any{"schema_version": 1,
		"base":         map[string]any{"asset_version_id": base},
		"donor":        map[string]any{"asset_version_id": donor},
		"confirmation": v2input.Ref{ArtifactID: owner, SHA256: sha}})
	if err != nil {
		t.Fatal(err)
	}
	request := GlareDonorDraftBeginRequest{Tenant: tenant, Project: uuid.New(),
		Shoot: uuid.New(), Snapshot: uuid.New(), Actor: auth.Identity{
			UserID: actor, SessionID: uuid.New()}, Key: "glare-begin-1", Ordinal: 3,
		Proposal: proposal, Pin: pin, SourceObservation: uuid.New(),
		DonorObservation: uuid.New(), ReservedBytes: minRestorationDraftReservation}
	tx := &restorationStoreTx{role: "photo_scheduler", accepted: true}
	beginner := SQLGlareDonorDraftBeginner{DB: restorationStoreDB{tx}}
	got, err := beginner.Begin(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	wantRun, err := v2input.RunID(request.Tenant, request.Actor.UserID, request.Key)
	if err != nil {
		t.Fatal(err)
	}
	wantWorkflow, err := v2input.WorkflowID(wantRun)
	if err != nil {
		t.Fatal(err)
	}
	if got.Run != wantRun || got.Workflow != wantWorkflow || got.BaseVersion != base ||
		got.DonorVersion != donor || tx.commits != 1 || tx.tenant != request.Tenant.String() ||
		len(tx.args) != 4 || tx.args[1] != string(proposal) ||
		tx.args[2] != pin.PolicySHA || tx.args[3] != request.ReservedBytes {
		t.Fatal("draft begin lost deterministic identity or quota binding")
	}
	var config struct {
		ProjectID  uuid.UUID `json:"project_id"`
		SnapshotID uuid.UUID `json:"input_snapshot_id"`
		Intent     struct {
			Run         uuid.UUID `json:"run_id"`
			Workflow    uuid.UUID `json:"workflow_id"`
			Shoot       uuid.UUID `json:"shoot_session_id"`
			Source      uuid.UUID `json:"source_observation_id"`
			Donor       uuid.UUID `json:"donor_observation_id"`
			ProposalSHA string    `json:"params_proposal_sha"`
		} `json:"v28_restoration"`
	}
	if json.Unmarshal([]byte(tx.args[0].(string)), &config) != nil ||
		config.ProjectID != request.Project || config.SnapshotID != request.Snapshot ||
		config.Intent.Run != wantRun || config.Intent.Workflow != wantWorkflow ||
		config.Intent.Shoot != request.Shoot ||
		config.Intent.Source != request.SourceObservation ||
		config.Intent.Donor != request.DonorObservation {
		t.Fatal("draft begin config lost request authority")
	}
	proposalSHA, err := canonical.Hash(json.RawMessage(proposal))
	if err != nil || config.Intent.ProposalSHA != proposalSHA {
		t.Fatal("proposal changed before durable replay", err)
	}
	wrongOwner, err := canonical.Marshal(map[string]any{"schema_version": 1,
		"base":         map[string]any{"asset_version_id": base},
		"donor":        map[string]any{"asset_version_id": donor},
		"confirmation": v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}})
	if err != nil {
		t.Fatal(err)
	}
	tx.queries = nil
	request.Proposal = wrongOwner
	if _, err := beginner.Begin(context.Background(), request); err != v2input.ErrInvalid || len(tx.queries) != 0 {
		t.Fatal("arbitrary confirmation owner reached SQL", err)
	}
	request.Proposal = proposal
	tx.queries = nil
	request.DonorObservation = request.SourceObservation
	if _, err := beginner.Begin(context.Background(), request); err != v2input.ErrInvalid || len(tx.queries) != 0 {
		t.Fatal("invalid selection reached SQL", err)
	}
}

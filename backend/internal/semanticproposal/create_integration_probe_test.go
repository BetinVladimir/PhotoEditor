package semanticproposal

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func probeCommittedCreateService(t *testing.T, ctx context.Context, service CreateService, who auth.Identity, request Request, owner *pgxpool.Pool, measuredPerson bool) {
	t.Helper()
	deployment := createDeploymentForProbe(t, service.Preparer)
	objects, ok := service.Preparer.Reader.Objects.(*objectstore.Store)
	if !ok {
		t.Fatal("physical create object store")
	}
	routed, err := deployment.Build(ctx, service.Preparer.Reader.DB, service.Preparer.Scheduler, objects, service.Preparer.Reader.Bucket)
	if err != nil {
		t.Fatal("project create installation", err)
	}
	unconfigured := service.Preparer
	unconfigured.Reader.ProjectID = uuid.New()
	other, err := NewProjectCreateService(ctx, []TransferDraftPreparer{unconfigured})
	if err != nil {
		t.Fatal("other project catalog installation", err)
	}
	if denied, err := other.Propose(ctx, who, request.TenantID, "proposal-publication", request.Intent); !errors.Is(err, v2qualification.ErrUnqualified) || denied.Status != 0 || denied.ResourceID != nil || len(denied.Body) != 0 {
		t.Fatal("unconfigured original project used another policy", err)
	}
	if denied, err := routed.Propose(ctx, who, uuid.New(), "proposal-publication", request.Intent); err == nil || denied.Status != 0 || denied.ResourceID != nil || len(denied.Body) != 0 {
		t.Fatal("foreign tenant resolved project policy", err)
	}
	probeRevokedHTTP := probeCreateHTTP(t, ctx, routed, service.Preparer, owner, who, request, measuredPerson)
	if os.Getenv("PHOTO_TEST_V27_PERSON_WIRE") == "1" {
		return
	}
	first, err := routed.Propose(ctx, who, request.TenantID, "proposal-publication", request.Intent)
	if err != nil || first.Status != 202 || first.ResourceID == nil || *first.ResourceID != request.RunID {
		t.Fatal("full ordinary create receipt", err)
	}
	var ticket v2input.JobTicket
	if json.Unmarshal(first.Body, &ticket) != nil || ticket.RunID != request.RunID || ticket.WorkflowID != request.WorkflowID || ticket.State != "queued" {
		t.Fatal("full ordinary create ticket")
	}
	startup := exec.CommandContext(ctx, "go", "test", "./cmd/photo", "-run", "^TestIntegrationSemanticCreateDeployment$", "-count=1", "-timeout=90s", "-v")
	startup.Dir = "../.."
	startup.Env = os.Environ()
	startupOutput, startupErr := startup.CombinedOutput()
	if startupErr != nil || !strings.Contains(string(startupOutput), "V27_CREATE_DEPLOYMENT_BUILD_OK") || !strings.Contains(string(startupOutput), "V27_CREATE_EXECUTABLE_REPLAY_SHUTDOWN_OK") {
		t.Fatal("create deployment startup probe failed", startupErr, string(startupOutput))
	}
	t.Log("V27_CREATE_DEPLOYMENT_BUILD_OK")
	t.Log("V27_CREATE_EXECUTABLE_REPLAY_SHUTDOWN_OK")
	for retry := 0; retry < 2; retry++ {
		replay, err := routed.Propose(ctx, who, request.TenantID, "proposal-publication", request.Intent)
		if err != nil || replay.Status != first.Status || replay.ResourceID == nil || *replay.ResourceID != *first.ResourceID || !bytes.Equal(replay.Body, first.Body) {
			t.Fatal("full ordinary create replay", retry, err)
		}
	}
	changed := request.Intent
	changed.Strength = .91
	if denied, err := routed.Propose(ctx, who, request.TenantID, "proposal-publication", changed); err == nil || denied.Status != 0 || denied.ResourceID != nil || len(denied.Body) != 0 {
		t.Fatal("changed create request recovered old receipt", err)
	}
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	denied, deniedErr := routed.Propose(ctx, who, request.TenantID, "proposal-publication", request.Intent)
	probeRevokedHTTP()
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	if deniedErr == nil || denied.Status != 0 || denied.ResourceID != nil || len(denied.Body) != 0 {
		t.Fatal("revoked create replay returned receipt", deniedErr)
	}
	var events, messages, receipts int
	if err := owner.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2 AND type='v2.run.queued'),
 (SELECT count(*) FROM photo.outbox WHERE tenant_id=$1 AND payload->>'run_id'=$2::text AND routing_key='v2.run.queued'),
 (SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND resource_id=$2 AND route='V2.Admit' AND response_status=202)`, request.TenantID, request.RunID).Scan(&events, &messages, &receipts); err != nil || events != 1 || messages != 1 || receipts != 1 {
		t.Fatal("full ordinary create duplicate effects", events, messages, receipts, err)
	}
	t.Log("V27_CREATE_COMMITTED_AND_REPLAYED_OK")
}

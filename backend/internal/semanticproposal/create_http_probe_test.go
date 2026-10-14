package semanticproposal

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"photoeditor/backend/internal/masknpy"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/config"
	"photoeditor/backend/internal/httpapi"
	"photoeditor/backend/internal/v2input"
)

// Uses a real listening HTTP server and the production access-token/session
// verifier. The first accepted request must create the parent, before direct
// service replay probes run.
func probeCreateHTTP(t *testing.T, ctx context.Context, service httpapi.SemanticPropose, preparer TransferDraftPreparer, owner *pgxpool.Pool, who auth.Identity, request Request, measuredPerson bool) func() {
	t.Helper()
	reader := preparer.Reader
	ordinaryRecovery := SQLOrdinaryParentRecovery{SQLNativePersonAdmissionRecovery: SQLNativePersonAdmissionRecovery{SQLNativeInitialPipelineRecovery: SQLNativeInitialPipelineRecovery{Preparer: preparer, CatalogForReference: (SQLNativeParentArtifactCatalogFactory{API: preparer.Reader.DB, Bucket: preparer.Reader.Bucket}).ForReference, Timeout: time.Minute}}}

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal("create HTTP signing key", err)
	}
	poolConfig := owner.Config().Copy()
	poolConfig.ConnConfig.RuntimeParams["role"] = "photo_auth"
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		t.Fatal("create HTTP auth pool unavailable")
	}
	t.Cleanup(pool.Close)
	authService := &auth.Service{DB: pool, Pepper: make([]byte, 32), Signer: auth.TokenSigner{Active: "fixture", Private: private, Public: map[string]ed25519.PublicKey{"fixture": public}, Issuer: "fixture", Audience: "fixture"}}
	var epoch int64
	if err := owner.QueryRow(ctx, "SELECT auth_epoch FROM photo.users WHERE id=$1", who.UserID).Scan(&epoch); err != nil {
		t.Fatal("create HTTP actor unavailable")
	}
	token, err := authService.Signer.Issue(who.UserID, who.SessionID, epoch)
	if err != nil {
		t.Fatal("create HTTP access token unavailable")
	}
	server := httptest.NewServer((&httpapi.Server{DB: reader.DB, Auth: authService, SemanticPropose: service, Config: config.Config{Environment: "development", AppOrigin: "http://localhost:8080", PortalOrigin: "http://localhost:3000", MaxJSONBytes: 65536}}).Routes())
	t.Cleanup(server.Close)
	client := server.Client()
	client.Timeout = 30 * time.Second
	body, err := json.Marshal(request.Intent)
	if err != nil {
		t.Fatal(err)
	}
	post := func(tenant uuid.UUID, access, key string, raw []byte, web bool) (int, []byte) {
		t.Helper()
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, server.URL+"/api/v1/tenants/"+tenant.String()+"/semantic-intents", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("Content-Type", "application/json")
		if web {
			r.Header.Set("Origin", "http://localhost:8080")
		} else {
			r.Header.Set("X-Photo-Client", "native")
		}
		if access != "" {
			r.Header.Set("Authorization", "Bearer "+access)
		}
		if key != "" {
			r.Header.Set("Idempotency-Key", key)
		}
		response, err := client.Do(r)
		if err != nil {
			t.Fatal("create HTTP request failed", err)
		}
		defer response.Body.Close()
		result, err := io.ReadAll(io.LimitReader(response.Body, 65537))
		if err != nil || len(result) > 65536 {
			t.Fatal("create HTTP response invalid")
		}
		return response.StatusCode, result
	}
	for _, negative := range []struct {
		access, key string
		body        []byte
		web         bool
		status      int
	}{
		{"", "proposal-publication", body, false, 401},
		{token, "proposal-publication", body, true, 403},
		{token, "", body, false, 400},
		{token, "proposal-publication", []byte(`{"unknown":true}`), false, 422},
	} {
		if status, _ := post(request.TenantID, negative.access, negative.key, negative.body, negative.web); status != negative.status {
			t.Fatal("create HTTP guard", status, negative.status)
		}
	}
	if status, _ := post(uuid.New(), token, "proposal-publication", body, false); status < 400 || status >= 500 {
		t.Fatal("create HTTP foreign tenant guard", status)
	}
	// Retire only the uniquely selected fixture root, retaining its exact
	// original expiry. A valid request with no live producer is unavailable,
	// never malformed and never admitted from the caller's reference mask.
	candidate, err := reader.transferTargetProtectionCandidate(ctx, who, request.TenantID, request.Intent.Reference)
	if err != nil {
		t.Fatal("create HTTP missing-producer fixture selection", err)
	}
	func() {
		var expires *time.Time
		if owner.QueryRow(ctx, "SELECT expires_at FROM photo.artifacts WHERE tenant_id=$1 AND id=$2", request.TenantID, candidate.root.ArtifactID).Scan(&expires) != nil {
			t.Fatal("create HTTP original producer expiry unavailable")
		}
		if _, err := owner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2", request.TenantID, candidate.root.ArtifactID); err != nil {
			t.Fatal(err)
		}
		defer func() {
			if _, err := owner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=$3 WHERE tenant_id=$1 AND id=$2", request.TenantID, candidate.root.ArtifactID, expires); err != nil {
				t.Error("create HTTP producer expiry restoration failed")
			}
		}()
		if got, err := reader.transferTargetProtectionCandidate(ctx, who, request.TenantID, request.Intent.Reference); !errors.Is(err, errTransferProtectionUnavailable) || got != (personCandidate{}) {
			t.Fatal("create HTTP absent producer classification", err)
		}
		if status, raw := post(request.TenantID, token, "proposal-publication", body, false); status != 503 || !bytes.Contains(raw, []byte("DEPENDENCY_UNAVAILABLE")) {
			t.Fatal("create HTTP missing producer response", status)
		}
	}()
	person, err := reader.personCandidate(ctx, who, request.TenantID, request.Intent.Reference, preparer.Configuration.PersonProtection.Expected)
	if err != nil {
		t.Fatal("create HTTP person fixture selection", err)
	}
	func() {
		var expires *time.Time
		if owner.QueryRow(ctx, "SELECT expires_at FROM photo.artifacts WHERE tenant_id=$1 AND id=$2", request.TenantID, person.root.ArtifactID).Scan(&expires) != nil {
			t.Fatal("create HTTP original person expiry unavailable")
		}
		if _, err := owner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2", request.TenantID, person.root.ArtifactID); err != nil {
			t.Fatal(err)
		}
		defer func() {
			// New native outputs must remain the sole candidate; never restore
			// old fixture evidence as the parent readiness transition.
			if measuredPerson && os.Getenv("PHOTO_TEST_V27_PERSON_WIRE") == "1" {
				return
			}
			if _, err := owner.Exec(ctx, "UPDATE photo.artifacts SET expires_at=$3 WHERE tenant_id=$1 AND id=$2", request.TenantID, person.root.ArtifactID, expires); err != nil {
				t.Error("create HTTP person expiry restoration failed")
			}
		}()
		if got, err := reader.personCandidate(ctx, who, request.TenantID, request.Intent.Reference, preparer.Configuration.PersonProtection.Expected); !errors.Is(err, errPersonProducerUnavailable) || got != (personCandidate{}) {
			t.Fatal("create HTTP absent person classification", err)
		}
		if status, raw := post(request.TenantID, token, "proposal-publication", body, false); status != 503 || !bytes.Contains(raw, []byte("DEPENDENCY_UNAVAILABLE")) {
			t.Fatal("create HTTP missing qualified person response", status)
		}
		if measuredPerson {
			personRun, _, err := personPreparationIDs(request.RunID)
			if err != nil {
				t.Fatal(err)
			}
			var originalHash string
			assertChild := func() {
				t.Helper()
				var hash, runState, taskState string
				var sealed bool
				var queuedEvents, queuedMessages, receipts int
				err := owner.QueryRow(ctx, `SELECT r.config_hash,r.status,r.graph_sealed,t.state,
      (SELECT count(*) FROM photo.run_events e WHERE e.tenant_id=r.tenant_id AND e.run_id=r.id AND e.type='v2.run.queued'),
      (SELECT count(*) FROM photo.outbox o WHERE o.tenant_id=r.tenant_id AND o.payload->>'run_id'=r.id::text AND o.routing_key='v2.run.queued'),
      (SELECT count(*) FROM photo.idempotency_records i WHERE i.tenant_id=r.tenant_id AND i.resource_id=r.id AND i.route='V2.Admit' AND i.response_status=202)
      FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id
      WHERE r.tenant_id=$1 AND r.id=$2 AND t.operation='mask.person' AND r.config_snapshot->'v27_person_prepare'->>'parent_run_id'=$3`, request.TenantID, personRun, request.RunID.String()).Scan(&hash, &runState, &sealed, &taskState, &queuedEvents, &queuedMessages, &receipts)
				if err != nil || runState != "queued" || !sealed || taskState != "ready" || queuedEvents != 1 || queuedMessages != 1 || receipts != 1 {
					t.Fatal("HTTP person prerequisite effects", err, runState, taskState, queuedEvents, queuedMessages, receipts)
				}
				if originalHash != "" && hash != originalHash {
					t.Fatal("HTTP person retry changed saved config")
				}
				originalHash = hash
			}
			assertChild()
			var detectorCount int
			if err := owner.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE tenant_id=$1 AND config_snapshot ? 'v27_detector_prepare'", request.TenantID).Scan(&detectorCount); err != nil || detectorCount != 0 {
				t.Fatal("person-only HTTP unexpectedly required detector", err, detectorCount)
			}
			var recoveryProject *uuid.UUID
			if err := preparer.Scheduler.QueryRow(ctx, "SELECT photo.next_v27_person_admission_project(NULL)").Scan(&recoveryProject); err != nil || recoveryProject == nil || *recoveryProject != reader.ProjectID {
				t.Fatal("standalone person recovery project discovery", err)
			}
			job := SQLNativePersonAdmissionRecovery{SQLNativeInitialPipelineRecovery: SQLNativeInitialPipelineRecovery{Preparer: preparer, CatalogForReference: (SQLNativeParentArtifactCatalogFactory{API: reader.DB, Bucket: reader.Bucket}).ForReference, Timeout: time.Minute}}
			if worked, err := job.ProcessOne(ctx); err != nil || !worked {
				t.Fatal("standalone person durable receipt recovery", err)
			}
			if worked, err := job.ProcessOne(ctx); err != nil || worked {
				t.Fatal("standalone person completed admission reclaimed", err)
			}
			var enabled, done bool
			var lease *uuid.UUID
			if err := owner.QueryRow(ctx, "SELECT v27_initial_recovery_enabled,v27_initial_recovery_done,v27_initial_recovery_token FROM photo.runs WHERE tenant_id=$1 AND id=$2", request.TenantID, personRun).Scan(&enabled, &done, &lease); err != nil || !enabled || !done || lease != nil {
				t.Fatal("standalone person durable admission completion", err)
			}
			var staleRecorded bool
			if err := preparer.Scheduler.QueryRow(ctx, "SELECT photo.record_v27_person_admission($1,$2,$3,'admitted')", request.TenantID, personRun, uuid.New()).Scan(&staleRecorded); err != nil || staleRecorded {
				t.Fatal("stale person recovery lease recorded", err)
			}
			assertChild()
			t.Log("V27_PERSON_STANDALONE_DURABLE_REPLAY_OK")
			if worked, err := ordinaryRecovery.ProcessOne(ctx); err == nil || !worked {
				t.Fatal("ordinary parent admitted before qualified person available", err)
			}
			var parentEnabled, parentDone bool
			if err := owner.QueryRow(ctx, "SELECT v27_parent_recovery_enabled,v27_parent_recovery_done FROM photo.runs WHERE tenant_id=$1 AND id=$2", request.TenantID, personRun).Scan(&parentEnabled, &parentDone); err != nil || !parentEnabled || parentDone {
				t.Fatal("ordinary parent retry checkpoint", err)
			}

			pendingChanged := request.Intent
			pendingChanged.Strength = .91
			pendingChangedBody, err := json.Marshal(pendingChanged)
			if err != nil {
				t.Fatal(err)
			}
			if status, _ := post(request.TenantID, token, "proposal-publication", pendingChangedBody, false); status != 409 {
				t.Fatal("pending person changed-body conflict", status)
			}
			assertChild()
			if status, raw := post(request.TenantID, token, "proposal-publication", body, false); status != 503 || !bytes.Contains(raw, []byte("DEPENDENCY_UNAVAILABLE")) {
				t.Fatal("HTTP person pending replay", status)
			}
			assertChild()
			t.Log("V27_CREATE_HTTP_MEASURED_PERSON_QUEUED_REPLAY_OK")
			if os.Getenv("PHOTO_TEST_V27_PERSON_WIRE") == "1" {
				probeCreatePersonNewOutputs(t, ctx, owner, request.TenantID, request.RunID)
				selected, err := reader.personCandidate(ctx, who, request.TenantID, request.Intent.Reference, preparer.Configuration.PersonProtection.Expected)
				if err != nil || selected.root.ArtifactID == person.root.ArtifactID || selected.root.ArtifactID == uuid.Nil {
					t.Fatal("parent did not select new native person", err)
				}
				t.Log("V27_CREATE_PERSON_NEW_CANDIDATE_SELECTED_OK")
			}
		}

	}()
	var effects int
	if err := owner.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2 AND type='v2.run.queued') +
 (SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND resource_id=$2 AND route='V2.Admit')`, request.TenantID, request.RunID).Scan(&effects); err != nil || effects != 0 {
		t.Fatal("create HTTP negative requests admitted parent before first success", effects, err)
	}
	if measuredPerson {
		personRun, _, err := personPreparationIDs(request.RunID)
		if err != nil {
			t.Fatal(err)
		}
		tx, err := owner.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err == nil {
			_, err = tx.Exec(ctx, "UPDATE photo.runs SET v27_parent_recovery_after=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2", request.TenantID, personRun)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		worked, recoverErr := ordinaryRecovery.ProcessOne(ctx)
		if os.Getenv("PHOTO_TEST_V27_PERSON_WIRE") == "1" {
			if !worked || !errors.Is(recoverErr, masknpy.ErrInvalid) {
				t.Fatal("uncovered new person admitted parent", recoverErr)
			}
			var parentDone bool
			if err := owner.QueryRow(ctx, "SELECT v27_parent_recovery_done FROM photo.runs WHERE tenant_id=$1 AND id=$2", request.TenantID, personRun).Scan(&parentDone); err != nil || parentDone {
				t.Fatal("uncovered parent completion", err)
			}
			if status, raw := post(request.TenantID, token, "proposal-publication", body, false); status != 503 || !bytes.Contains(raw, []byte("DEPENDENCY_UNAVAILABLE")) {
				t.Fatal("uncovered new person HTTP refusal", status)
			}
			t.Log("V27_CREATE_PERSON_UNCOVERED_PARENT_REFUSED_OK")
			return func() {}
		}
		if recoverErr != nil || !worked {
			t.Fatal("ordinary parent scheduler admission after canonical evidence", recoverErr)
		}
		if worked, err := ordinaryRecovery.ProcessOne(ctx); err != nil || worked {
			t.Fatal("ordinary parent completed recovery reclaimed", err)
		}
		var done bool
		if err := owner.QueryRow(ctx, "SELECT v27_parent_recovery_done FROM photo.runs WHERE tenant_id=$1 AND id=$2", request.TenantID, personRun).Scan(&done); err != nil || !done {
			t.Fatal("ordinary parent durable completion", err)
		}
		t.Log("V27_ORDINARY_PARENT_DURABLE_CONTINUATION_OK")
	}
	status, first := post(request.TenantID, token, "proposal-publication", body, false)
	var ticket v2input.JobTicket
	if status != 202 || json.Unmarshal(first, &ticket) != nil || ticket.RunID != request.RunID || ticket.WorkflowID != request.WorkflowID || ticket.State != "queued" {
		t.Fatal("create HTTP initial admission", status)
	}
	if status, replay := post(request.TenantID, token, "proposal-publication", body, false); status != 202 || !bytes.Equal(replay, first) {
		t.Fatal("create HTTP replay", status)
	}
	changed := request.Intent
	changed.Strength = .91
	changedBody, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if status, _ := post(request.TenantID, token, "proposal-publication", changedBody, false); status != 409 {
		t.Fatal("create HTTP changed-body conflict", status)
	}
	return func() {
		if status, _ := post(request.TenantID, token, "proposal-publication", body, false); status != 401 {
			t.Fatal("create HTTP revoked session replay", status)
		}
		t.Log("V27_CREATE_HTTP_AUTH_ADMISSION_REPLAY_OK")
	}
}

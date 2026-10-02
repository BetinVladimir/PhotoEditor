package v2capture

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
	"time"
)

// Focused SQL gate fixture: workflow/frame rows are synthetic and rolled back.
// All foreign keys are checked IMMEDIATE. Base identities are committed
// so a second connection can exercise locks. This is not end-to-end adoption.
func TestIntegrationCaptureLiveEvidenceGate(t *testing.T) {
	for _, historical := range []bool{false, true} {
		name := "current_frame"
		if historical {
			name = "historical_coverage"
		}
		t.Run(name, func(t *testing.T) { testCaptureLiveEvidenceGate(t, historical) })
	}
}

func testCaptureLiveEvidenceGate(t *testing.T, historical bool) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var actor, tenant, project, collection, session, person, grant uuid.UUID
	var epoch int64
	must := func(query string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	scan := func(query string, dst any, args ...any) {
		t.Helper()
		if err := tx.QueryRow(ctx, query, args...).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	if err = tx.QueryRow(ctx, `SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`, uuid.NewString()+"@test.example").Scan(&actor, &epoch); err != nil {
		t.Fatal(err)
	}
	scan(`SELECT id FROM photo.tenants WHERE owner_user_id=$1`, &tenant, actor)
	scan(`INSERT INTO photo.projects(tenant_id,name) VALUES($1,'Gate') RETURNING id`, &project, tenant)
	scan(`INSERT INTO photo.collections(tenant_id,project_id,name) VALUES($1,$2,'Gate') RETURNING id`, &collection, tenant, project)
	scan(`INSERT INTO photo.shoot_sessions(tenant_id,project_id,collection_id,name,state,policy,consent_epoch,capture_policy) VALUES($1,$2,$3,'Gate','open','{}',1,'{}') RETURNING id`, &session, tenant, project, collection)
	scan(`INSERT INTO photo.project_persons(tenant_id,project_id,label,state,consent_epoch) VALUES($1,$2,'Gate','active',1) RETURNING id`, &person, tenant, project)
	scan(`INSERT INTO photo.consent_grants(tenant_id,project_id,purpose,person_id,status,epoch,policy_version,actor_id) VALUES($1,$2,'identity_matching',$3,'granted',1,'test',$4) RETURNING id`, &grant, tenant, project, person, actor)
	// Commit only valid base rows; graph references below remain uncommitted.
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	tx, err = db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	var snapshot, run, paramsArtifact, task, result uuid.UUID
	sha := strings.Repeat("a", 64)
	scan(`INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'v2','user',$2,$3,0) RETURNING id`, &snapshot, tenant, actor, sha)
	scan(`INSERT INTO photo.runs(created_at,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) VALUES('1970-01-01'::timestamptz,$1,'v2','running',$2,jsonb_build_object('project_id',$3::text),$4,$5,'fail_fast',true) RETURNING id`, &run, tenant, snapshot, project, sha, actor)
	workflow := uuid.New()
	must(`INSERT INTO photo.v2_workflows(id,tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES($1,$2,$3,$4,$5,'capture','v2.2',$6)`, workflow, tenant, project, run, snapshot, sha)
	scan(`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES($1,'test',$2,$3,2,'application/json','v1',now()) RETURNING id`, &paramsArtifact, tenant, "gate/"+uuid.NewString(), sha)
	scan(`INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,attempt,fencing_token,execution_profile) VALUES($1,$2,'capture.assess:0','capture.assess','1','gpu','blocked','[]',$3,$4,1,1,'{"backend":"cuda","renderer_version":"v2.2"}') RETURNING id`, &task, tenant, run, paramsArtifact, sha)
	must(`INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,backend,profile) VALUES($1,$2,1,1,$3,now(),'cuda','{}')`, tenant, task, uuid.New())
	var root uuid.UUID
	rootKey := "report/" + uuid.NewString()
	scan(`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES($1,'test',$2,$3,2,'application/json','v1',now(),'document',$4,1,1) RETURNING id`, &root, tenant, rootKey, sha, task)
	must(`INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version) VALUES($1,$2,1,1,$3,$4,$5,$6,'document','test',$7,$4,2,'application/json','ready','v1')`, tenant, task, uuid.New(), sha, uuid.NewString(), root, rootKey)
	scan(`INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,review_required,result_digest) VALUES($1,$2,1,1,jsonb_build_array(jsonb_build_object('artifact_id',$4::text,'sha256',$3::text)),'{}',false,$3) RETURNING id`, &result, tenant, task, sha, root)
	must(`UPDATE photo.tasks SET canonical_result_id=$1,state='succeeded' WHERE id=$2`, result, task)
	var asset, version, device, receipt, reservation uuid.UUID
	upload := uuid.New()
	scan(`INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`, &asset, tenant, project, paramsArtifact)
	scan(`INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,'{}','accepted') RETURNING id`, &version, tenant, asset, paramsArtifact, sha)
	scan(`INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,state,expires_at) VALUES($1,'user',$2,'upload',$3,2,1,'consumed',now()+interval '1 hour') RETURNING id`, &reservation, tenant, actor, upload)
	must(`INSERT INTO photo.upload_sessions(id,tenant_id,project_id,collection_id,principal_type,principal_id,expected_size,actual_size,filename,object_key,state,expires_at,verified_version_id,reservation_id) VALUES($1,$2,$3,$4,'user',$5,2,2,'gate.jpg',$6,'verified',now()+interval '1 hour',$7,$8)`, upload, tenant, project, collection, actor, "upload/"+uuid.NewString(), version, reservation)
	scan(`INSERT INTO photo.capture_devices(tenant_id,project_id,label,public_key,created_by) VALUES($1,$2,'Gate',$3,$4) RETURNING id`, &device, tenant, project, strings.Repeat("A", 43), actor)
	scan(`INSERT INTO photo.capture_receipts(tenant_id,project_id,device_id,session_id,sequence,content_sha,upload_id) VALUES($1,$2,$3,$4,0,$5,$6) RETURNING id`, &receipt, tenant, project, device, session, sha, upload)
	must(`INSERT INTO photo.capture_receipt_assets(tenant_id,project_id,receipt_id,asset_version_id) VALUES($1,$2,$3,$4)`, tenant, project, receipt, version)
	must(`UPDATE photo.capture_assessment_jobs SET state='queued',workflow_id=$6,preparation_workflow_id=$6,
 selection_seed=jsonb_build_object('consent_epoch','1'),
 consent_seed=jsonb_build_object('grants',CASE WHEN $9::boolean THEN '[]'::jsonb ELSE jsonb_build_array(jsonb_build_object('person_id',$7::text,'consent_id',$8::text,'epoch','1')) END),consent_people='{}',
 coverage_seed=CASE WHEN $9::boolean THEN jsonb_build_array(jsonb_build_object('identity_evidence',jsonb_build_array(jsonb_build_object('person_id',$7::text,'consent_id',$8::text,'epoch','1')))) ELSE '[]'::jsonb END
 WHERE tenant_id=$1 AND project_id=$2 AND receipt_id=$3 AND session_id=$4 AND asset_version_id=$5`, tenant, project, receipt, session, version, workflow, person, grant, historical)

	// Exercise the SQL adoption boundary independently of the rejection fixture.
	// Savepoint rollback retains the original task and admission for later tests.
	for _, delayed := range []bool{false, true} {
		must(`SAVEPOINT accepted_report`)
		if delayed {
			must(`UPDATE photo.capture_assessment_jobs SET created_at=clock_timestamp()-interval '31 seconds' WHERE tenant_id=$1 AND preparation_workflow_id=$2`, tenant, workflow)
		}
		encode := func(value any) (string, string) {
			t.Helper()
			body, e := canonical.Marshal(value)
			if e != nil {
				t.Fatal(e)
			}
			digest := sha256.Sum256(body)
			return string(body), hex.EncodeToString(digest[:])
		}
		contextBody, contextSHA := encode(map[string]any{"schema_version": 1, "session_id": session.String()})
		var contextArtifact, adoptParams, adoptTask, adoptRoot, adoptResult uuid.UUID
		scan(`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES($1,'test',$2,$3,$4,'application/json','v1',now()) RETURNING id`, &contextArtifact, tenant, "context/"+uuid.NewString(), contextSHA, len(contextBody))
		binding := map[string]any{"asset_version_id": version.String()}
		paramsBody, paramsSHA := encode(map[string]any{"binding": binding, "context": map[string]any{"artifact_id": contextArtifact.String(), "sha256": contextSHA}, "policy": map[string]any{"checks": []string{"focus", "clipping"}}})
		reportBody, reportSHA := encode(map[string]any{"schema_version": 1, "binding": binding, "checks": []any{map[string]any{"code": "focus", "status": "pass", "score": 0.9, "face_id": nil}, map[string]any{"code": "clipping", "status": "pass", "score": 0.05, "face_id": nil}}})
		scan(`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES($1,'test',$2,$3,$4,'application/json','v1',now()) RETURNING id`, &adoptParams, tenant, "params/"+uuid.NewString(), paramsSHA, len(paramsBody))
		scan(`INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,attempt,fencing_token,execution_profile) VALUES($1,$2,'capture.assess:1','capture.assess','1','gpu','blocked','[]',$3,$4,1,1,'{"backend":"cuda","renderer_version":"v2.2"}') RETURNING id`, &adoptTask, tenant, run, adoptParams, paramsSHA)
		must(`INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,backend,profile) VALUES($1,$2,1,1,$3,now(),'cuda','{}')`, tenant, adoptTask, uuid.New())
		must(`INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal) VALUES($1,$2,$3,$4,$5,'CaptureContext/1',0)`, tenant, project, adoptTask, contextArtifact, contextSHA)
		key := "accepted/" + uuid.NewString()
		scan(`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES($1,'test',$2,$3,$4,'application/json','v1',now(),'document',$5,1,1) RETURNING id`, &adoptRoot, tenant, key, reportSHA, len(reportBody), adoptTask)
		must(`INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version) VALUES($1,$2,1,1,$3,$4,$5,$6,'document','test',$7,$8,$9,'application/json','ready','v1')`, tenant, adoptTask, uuid.New(), sha, uuid.NewString(), adoptRoot, key, reportSHA, len(reportBody))
		scan(`INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,review_required,result_digest) VALUES($1,$2,1,1,jsonb_build_array(jsonb_build_object('artifact_id',$4::text,'sha256',$3::text)),'{}',false,$3) RETURNING id`, &adoptResult, tenant, adoptTask, reportSHA, adoptRoot)
		must(`UPDATE photo.tasks SET canonical_result_id=$1,state='succeeded' WHERE id=$2`, adoptResult, adoptTask)
		inventory, inventorySHA := encode([]any{map[string]any{"artifact_id": adoptRoot.String(), "sha256": reportSHA, "purpose": sha, "ordinal": 0}})
		must(`SET LOCAL ROLE photo_scheduler`)
		var adopted bool

		// Generic adoption writes its marker before capture-specific byte checks.
		// A rejected context must roll back that marker and every inventory link.
		must(`SAVEPOINT invalid_context`)
		_, badContext := tx.Exec(ctx, `SELECT photo.adopt_capture_report($1,$2,1,1,$3,$4::jsonb,$5,$6,$7,$8)`, tenant, adoptTask, adoptResult, inventory, inventorySHA, paramsBody, contextBody+" ", reportBody)
		var bytesError *pgconn.PgError
		if !errors.As(badContext, &bytesError) || bytesError.Code != "23514" || bytesError.Message != "CAPTURE_ADOPTION_BYTES_INVALID" {
			t.Fatalf("altered context bytes accepted: %v", badContext)
		}
		must(`ROLLBACK TO SAVEPOINT invalid_context`)
		must(`RELEASE SAVEPOINT invalid_context`)
		must(`RESET ROLE`)
		var markerCount int
		scan(`SELECT count(*) FROM photo.inbox WHERE tenant_id=$1 AND consumer='v2-adopt' AND event_id=photo.v2_adoption_event_id($2,$3)`, &markerCount, tenant, adoptTask, adoptResult)
		if markerCount != 0 {
			t.Fatal("failed capture adoption retained marker")
		}
		var linkCount int
		scan(`SELECT count(*) FROM photo.v2_artifact_links WHERE tenant_id=$1 AND artifact_id=$2`, &linkCount, tenant, adoptRoot)
		if linkCount != 0 {
			t.Fatal("failed capture adoption retained inventory links")
		}
		must(`SET LOCAL ROLE photo_scheduler`)
		scan(`SELECT photo.adopt_capture_report($1,$2,1,1,$3,$4::jsonb,$5,$6,$7,$8)`, &adopted, tenant, adoptTask, adoptResult, inventory, inventorySHA, paramsBody, contextBody, reportBody)
		if !adopted {
			t.Fatal("valid report not adopted")
		}
		scan(`SELECT photo.adopt_capture_report($1,$2,1,1,$3,$4::jsonb,$5,$6,$7,$8)`, &adopted, tenant, adoptTask, adoptResult, inventory, inventorySHA, paramsBody, contextBody, reportBody)
		if adopted {
			t.Fatal("report adopted twice")
		}
		must(`RESET ROLE`)
		var validAlert bool
		scan(`SELECT count(*)=1 AND bool_and(receipt_id=$3 AND qa_stale=$4 AND severity=CASE WHEN $4 THEN 'warning' ELSE 'info' END AND technical_quality=0.9 AND quality_report_sha=$5) FROM photo.capture_alerts WHERE tenant_id=$1 AND report_artifact_id=$2`, &validAlert, tenant, adoptRoot, receipt, delayed, reportSHA)
		if !validAlert {
			t.Fatal("adopted report alert binding, quality, or delay incorrect")
		}
		must(`ROLLBACK TO SAVEPOINT accepted_report`)
		must(`RELEASE SAVEPOINT accepted_report`)
	}
	must(`SET CONSTRAINTS ALL IMMEDIATE`)
	authSession := uuid.New()
	must(`INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`, authSession, actor, auth.HashSecret(uuid.NewString()), uuid.New())
	var alert, confirmation, replayConfirmation, otherReceipt uuid.UUID
	scan(`INSERT INTO photo.capture_alerts(tenant_id,project_id,session_id,asset_version_id,report_artifact_id,severity,technical_quality,quality_metric,quality_report_sha,receipt_id,qa_stale) VALUES($1,$2,$3,$4,$5,'warning',0.8,'capture.technical_quality/1',$6,$7,true) RETURNING id`, &alert, tenant, project, session, version, root, sha, receipt)
	scan(`INSERT INTO photo.capture_receipts(tenant_id,project_id,device_id,session_id,sequence,content_sha,upload_id) VALUES($1,$2,$3,$4,1,$5,$6) RETURNING id`, &otherReceipt, tenant, project, device, session, sha, upload)
	must(`INSERT INTO photo.capture_receipt_assets(tenant_id,project_id,receipt_id,asset_version_id) VALUES($1,$2,$3,$4)`, tenant, project, otherReceipt, version)
	must(`SELECT set_config('app.tenant_id',$1,true)`, tenant.String())
	must(`SET LOCAL ROLE photo_api`)
	scan(`SELECT photo.confirm_capture_frame($1,$2,$3,$4,$5)`, &confirmation, tenant, receipt, alert, actor, authSession)
	scan(`SELECT photo.confirm_capture_frame($1,$2,$3,$4,$5)`, &replayConfirmation, tenant, receipt, alert, actor, authSession)
	if confirmation == uuid.Nil || confirmation != replayConfirmation {
		t.Fatal("confirmation replay changed identity")
	}
	must(`SAVEPOINT wrong_receipt`)
	_, wrong := tx.Exec(ctx, `SELECT photo.confirm_capture_frame($1,$2,$3,$4,$5)`, tenant, otherReceipt, alert, actor, authSession)
	var wrongError *pgconn.PgError
	if !errors.As(wrong, &wrongError) || wrongError.Code != "23514" {
		t.Fatalf("receipt substitution accepted: %v", wrong)
	}
	must(`ROLLBACK TO SAVEPOINT wrong_receipt`)
	must(`RELEASE SAVEPOINT wrong_receipt`)
	var acknowledged []byte
	scan(`SELECT photo.acknowledge_capture_alert($1,$2,1,$3,$4)`, &acknowledged, tenant, alert, actor, authSession)
	var public map[string]any
	if err = json.Unmarshal(acknowledged, &public); err != nil {
		t.Fatal(err)
	}
	if public["revision"] != "2" || public["receipt_id"] != receipt.String() || public["qa_stale"] != true || public["acknowledged_by"] != actor.String() {
		t.Fatalf("invalid alert response %s", acknowledged)
	}
	for _, private := range []string{"technical_quality", "quality_metric", "quality_report_sha"} {
		if _, exposed := public[private]; exposed {
			t.Fatalf("private field exposed: %s", private)
		}
	}
	must(`SAVEPOINT stale_alert`)
	_, stale := tx.Exec(ctx, `SELECT photo.acknowledge_capture_alert($1,$2,1,$3,$4)`, tenant, alert, actor, authSession)
	var staleError *pgconn.PgError
	if !errors.As(stale, &staleError) || staleError.Code != "40001" {
		t.Fatalf("stale revision accepted: %v", stale)
	}
	must(`ROLLBACK TO SAVEPOINT stale_alert`)
	must(`RELEASE SAVEPOINT stale_alert`)
	must(`RESET ROLE`)
	var confirmationCount int
	scan(`SELECT count(*) FROM photo.capture_frame_confirmations WHERE tenant_id=$1`, &confirmationCount, tenant)
	if confirmationCount != 1 {
		t.Fatalf("confirmation rows=%d", confirmationCount)
	}

	must(`UPDATE photo.shoot_sessions SET capture_policy=jsonb_build_object('calibration',jsonb_build_object('artifact_id',$2::text,'sha256',$3::text)) WHERE id=$1`, session, paramsArtifact, sha)
	must(`SET LOCAL ROLE photo_fn_owner`)
	var reportReferenced, calibrationReferenced bool
	scan(`SELECT photo.v2_artifact_has_references($1,$2)`, &reportReferenced, tenant, root)
	scan(`SELECT photo.v2_artifact_has_references($1,$2)`, &calibrationReferenced, tenant, paramsArtifact)
	if !reportReferenced || !calibrationReferenced {
		t.Fatalf("Capture GC roots missing: report=%v calibration=%v", reportReferenced, calibrationReferenced)
	}
	must(`RESET ROLE`)
	// Report retention must affect the actual global GC claimant, not just the
	// helper. Backdate only test rows and roll all claims back immediately.
	must(`SAVEPOINT capture_gc`)
	must(`UPDATE photo.artifacts SET expires_at='1900-01-01'::timestamptz WHERE id=$1`, root)
	var orphan, claimed uuid.UUID
	scan(`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,expires_at) VALUES($1,'test',$2,$3,2,'application/json','v1',now(),'1901-01-01'::timestamptz) RETURNING id`, &orphan, tenant, "gc-orphan/"+uuid.NewString(), sha)
	must(`SET LOCAL ROLE photo_maintenance`)
	scan(`SELECT artifact_id FROM photo.claim_artifact_gc()`, &claimed)
	if claimed != orphan {
		t.Fatalf("GC did not skip older referenced report: claimed=%s expected orphan=%s", claimed, orphan)
	}
	must(`RESET ROLE`)
	var retained, leased bool
	scan(`SELECT state='ready' AND gc_token IS NULL FROM photo.artifacts WHERE id=$1`, &retained, root)
	scan(`SELECT state='deleting' AND gc_token IS NOT NULL AND gc_lease_until>clock_timestamp() FROM photo.artifacts WHERE id=$1`, &leased, orphan)
	if !retained || !leased {
		t.Fatal("invalid report retention/orphan GC lease")
	}
	must(`ROLLBACK TO SAVEPOINT capture_gc`)
	must(`RELEASE SAVEPOINT capture_gc`)

	must(`SET LOCAL ROLE photo_fn_owner`)
	must(`SELECT photo.assert_capture_live_evidence($1,$2)`, tenant, workflow)
	must(`RESET ROLE`)
	probe, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = probe.Exec(ctx, `SET LOCAL lock_timeout='150ms'`); err != nil {
		probe.Rollback(ctx)
		t.Fatal(err)
	}
	_, blocked := probe.Exec(ctx, `UPDATE photo.consent_grants SET status='revoked',epoch=epoch+1 WHERE id=$1`, grant)
	var lockError *pgconn.PgError
	if !errors.As(blocked, &lockError) || lockError.Code != "55P03" {
		probe.Rollback(ctx)
		t.Fatalf("revocation bypassed held grant lock: %v", blocked)
	}
	if err = probe.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	reject := func(change string, args []any, code string) {
		t.Helper()
		must(`SAVEPOINT gate_case`)
		must(change, args...)
		must(`SET LOCAL ROLE photo_fn_owner`)
		_, err := tx.Exec(ctx, `SELECT photo.assert_capture_live_evidence($1,$2)`, tenant, workflow)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "23514" || pg.Message != code {
			t.Fatalf("expected %s got %v", code, err)
		}
		must(`ROLLBACK TO SAVEPOINT gate_case`)
		must(`RELEASE SAVEPOINT gate_case`)
	}
	for _, role := range []string{"photo_api", "photo_worker", "photo_scheduler"} {
		must(`SAVEPOINT denied_role`)
		// Role names come exclusively from this fixed test allowlist.
		must(`SET LOCAL ROLE ` + role)
		_, err := tx.Exec(ctx, `SELECT photo.assert_capture_live_evidence($1,$2)`, tenant, workflow)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || pg.Code != "42501" {
			t.Fatalf("role %s unexpectedly called private gate: %v", role, err)
		}
		must(`ROLLBACK TO SAVEPOINT denied_role`)
		must(`RELEASE SAVEPOINT denied_role`)
	}
	reject(`UPDATE photo.consent_grants SET status='revoked',epoch=epoch+1 WHERE id=$1`, []any{grant}, "V24_IDENTITY_CONSENT_REVOKED")
	reject(`UPDATE photo.consent_grants SET epoch=epoch+1 WHERE id=$1`, []any{grant}, "V24_IDENTITY_CONSENT_REVOKED")
	reject(`UPDATE photo.shoot_sessions SET consent_epoch=consent_epoch+1 WHERE id=$1`, []any{session}, "V24_CAPTURE_CONTEXT_STALE")
	must(`SET LOCAL ROLE photo_fn_owner`)
	must(`SELECT photo.assert_capture_live_evidence($1,$2)`, tenant, workflow)
	must(`RESET ROLE`)
	must(`SET LOCAL ROLE photo_scheduler`)
	var rejected bool
	scan(`SELECT photo.reject_v2_adoption($1,$2,$3,'CONSENT_REVOKED')`, &rejected, tenant, task, result)
	if !rejected {
		t.Fatal("capture rejection not accepted")
	}
	must(`RESET ROLE`)
	var failed bool
	scan(`SELECT state='failed' AND error_code='CONSENT_REVOKED' AND workflow_id IS NULL AND lease_token IS NULL AND lease_until IS NULL FROM photo.capture_assessment_jobs WHERE tenant_id=$1 AND preparation_workflow_id=$2`, &failed, tenant, workflow)
	if !failed {
		t.Fatal("admission left active after rejection")
	}
	var cancelling bool
	scan(`SELECT status='cancelling' AND cancel_requested FROM photo.runs WHERE id=$1`, &cancelling, run)
	if !cancelling {
		t.Fatal("run not cancelling")
	}
	var eventsBefore, eventsAfter int
	scan(`SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2 AND type='v2.adoption.failed'`, &eventsBefore, tenant, run)
	must(`SET LOCAL ROLE photo_scheduler`)
	scan(`SELECT photo.reject_v2_adoption($1,$2,$3,'CONSENT_REVOKED')`, &rejected, tenant, task, result)
	if rejected {
		t.Fatal("duplicate rejection accepted")
	}
	must(`RESET ROLE`)
	scan(`SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2 AND type='v2.adoption.failed'`, &eventsAfter, tenant, run)
	if eventsBefore != 1 || eventsAfter != eventsBefore {
		t.Fatalf("duplicate events: before=%d after=%d", eventsBefore, eventsAfter)
	}
	must(`SET LOCAL ROLE photo_scheduler`)
	var finalized int
	scan(`SELECT photo.finalize_run_cancellation()`, &finalized)
	must(`RESET ROLE`)
	var cancelled bool
	scan(`SELECT status='cancelled' AND graph_sealed FROM photo.runs WHERE id=$1`, &cancelled, run)
	if finalized != 1 || !cancelled {
		t.Fatalf("cancellation not finalized: count=%d cancelled=%v", finalized, cancelled)
	}
	// Physical metadata purge boundary after synthetic storage-cleanup approval.
	// Savepoint rollback preserves committed base identities and lock-release test.
	must(`SAVEPOINT capture_purge`)
	must(`UPDATE photo.tenants SET status='deleting' WHERE id=$1`, tenant)
	var deletion uuid.UUID
	cleanupToken := uuid.New()
	scan(`INSERT INTO photo.deletion_jobs(tenant_id,resource_type,resource_id,status,requested_by,cleanup_token,cleanup_lease_until) VALUES($1,'tenant',$1,'running',$2,$3,clock_timestamp()+interval '1 minute') RETURNING id`, &deletion, tenant, actor, cleanupToken)
	for _, role := range []string{"photo_api", "photo_worker", "photo_scheduler"} {
		must(`SAVEPOINT denied_purge_role`)
		must(`SET LOCAL ROLE ` + role)
		_, denied := tx.Exec(ctx, `SELECT photo.finish_tenant_deletion($1,$2)`, deletion, cleanupToken)
		var deniedError *pgconn.PgError
		if !errors.As(denied, &deniedError) || deniedError.Code != "42501" {
			t.Fatalf("%s unexpectedly called metadata purge: %v", role, denied)
		}
		must(`ROLLBACK TO SAVEPOINT denied_purge_role`)
		must(`RELEASE SAVEPOINT denied_purge_role`)
	}
	must(`SET LOCAL ROLE photo_maintenance`)
	var purged bool
	scan(`SELECT photo.finish_tenant_deletion($1,$2)`, &purged, deletion, uuid.New())
	if purged {
		t.Fatal("purge accepted wrong cleanup token")
	}
	scan(`SELECT photo.finish_tenant_deletion($1,$2)`, &purged, deletion, cleanupToken)
	if !purged {
		t.Fatal("Capture metadata purge refused valid cleanup")
	}
	must(`RESET ROLE`)
	for _, table := range []string{"capture_frame_confirmations", "capture_assessment_jobs", "capture_alerts", "capture_receipt_assets", "capture_receipts", "capture_devices", "shoot_sessions", "project_persons", "consent_grants", "v2_workflows", "artifacts"} {
		var count int
		// Fixed test-only allowlist, never supplied by a caller.
		scan(`SELECT count(*) FROM photo.`+table+` WHERE tenant_id=$1`, &count, tenant)
		if count != 0 {
			t.Fatalf("purge retained %s rows=%d", table, count)
		}
	}
	var lifecycleFinished bool
	scan(`SELECT t.status='deleted' AND j.status='completed' AND j.cleanup_token IS NULL AND j.cleanup_lease_until IS NULL FROM photo.tenants t JOIN photo.deletion_jobs j ON j.tenant_id=t.id WHERE t.id=$1 AND j.id=$2`, &lifecycleFinished, tenant, deletion)
	if !lifecycleFinished {
		t.Fatal("purge did not finish tenant/job lifecycle")
	}
	must(`ROLLBACK TO SAVEPOINT capture_purge`)
	must(`RELEASE SAVEPOINT capture_purge`)
	if err = tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	probe, err = db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Rollback(context.Background())
	if _, err = probe.Exec(ctx, `SET LOCAL lock_timeout='150ms'`); err != nil {
		t.Fatal(err)
	}
	updated, err := probe.Exec(ctx, `UPDATE photo.consent_grants SET status='revoked',epoch=epoch+1 WHERE id=$1`, grant)
	if err != nil || updated.RowsAffected() != 1 {
		t.Fatalf("revocation after lock release: rows=%d err=%v", updated.RowsAffected(), err)
	}
}

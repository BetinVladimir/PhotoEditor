package provenance

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// Controlled canonical export rows exercise real SQL state/quota transitions.
// No native image worker or object deletion is simulated as successful evidence:
// finish_document_cleanup below represents an already acknowledged object purge.
// Everything is rolled back; the fixture requires an isolated migrated admin DB.
func TestIntegrationSigningLineagePublicationLifecycle(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
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
	exec := func(t *testing.T, query string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	scan := func(t *testing.T, query string, dst any, args ...any) {
		t.Helper()
		if err := tx.QueryRow(ctx, query, args...).Scan(dst); err != nil {
			t.Fatal(err)
		}
	}
	var user, tid, session, project, snapshot, run, params, task, source, result, asset, version, export uuid.UUID
	var epoch int64
	if err := tx.QueryRow(ctx, `SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`, uuid.NewString()+"@lineage.test").Scan(&user, &epoch); err != nil {
		t.Fatal(err)
	}
	scan(t, `SELECT id FROM photo.tenants WHERE owner_user_id=$1`, &tid, user)
	session = uuid.New()
	exec(t, `INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`, session, user, auth.HashSecret(uuid.NewString()), uuid.New())
	exec(t, `UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','1000000'::jsonb) WHERE id=$1`, tid)
	scan(t, `INSERT INTO photo.projects(tenant_id,name,visibility,download_allowed) VALUES($1,'Lineage SQL fixture','restricted',true) RETURNING id`, &project, tid)
	sha := strings.Repeat("b", 64)
	scan(t, `INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'input','user',$2,$3,1) RETURNING id`, &snapshot, tid, user, sha)
	scan(t, `INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES($1,'preview','completed',$2,'{}',$3,$4) RETURNING id`, &run, tid, snapshot, sha, user)
	scan(t, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,state) VALUES($1,'test-bucket',$2,$3,2,'application/json','params-v1',now(),'ready') RETURNING id`, &params, tid, uuid.NewString(), sha)
	scan(t, `INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,attempt,fencing_token,execution_profile) VALUES($1,$2,'media.preview:0','media.preview','1','media','ready','[]',$3,$4,1,1,'{"renderer_version":"v2.2"}') RETURNING id`, &task, tid, run, params, sha)
	exec(t, `INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES($1,$2,1,1,$3,now(),now(),'cpu','{}')`, tid, task, uuid.New())
	sourceKey := "lineage-source/" + uuid.NewString()
	scan(t, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence,state) VALUES($1,'test-bucket',$2,'source-v1',$3,42,'image/jpeg','derivative',now(),$4,1,1,'ready') RETURNING id`, &source, tid, sourceKey, sha, task)
	scan(t, `INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES($1,$2,1,1,jsonb_build_array(jsonb_build_object('artifact_id',$3::text,'sha256',$4::text)),'{}',$4) RETURNING id`, &result, tid, task, source, sha)
	exec(t, `UPDATE photo.tasks SET state='succeeded',canonical_result_id=$1 WHERE id=$2`, result, task)
	scan(t, `INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`, &asset, tid, project, source)
	scan(t, `INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state,created_by_run_id) VALUES($1,$2,$3,$4,'{}','accepted',$5) RETURNING id`, &version, tid, asset, source, sha, run)
	scan(t, `INSERT INTO photo.exports(tenant_id,asset_version_id,preset_hash,artifact_id,status,task_id,metadata_policy) VALUES($1,$2,$3,$4,'ready',$5,'{}') RETURNING id`, &export, tid, version, sha, source, task)
	exec(t, `SET CONSTRAINTS ALL IMMEDIATE`)
	body, expected := contractBytes(t, SigningLineage{Schema: 1, SourceHashes: []string{sha}, Actions: []SigningAction{}})
	expected.ArtifactID = uuid.NewSHA1(export, []byte("signing:lineage:"+expected.SHA256))
	who := auth.Identity{UserID: user, SessionID: session, Epoch: epoch}
	ready := ReadySigningExport{Tenant: tid, Project: project, ExportID: export, Version: version, Task: task, Run: run, Ref: v2input.Ref{ArtifactID: source, SHA256: sha}, Object: objectstore.Object{Key: sourceKey, VersionID: "source-v1", SHA256: sha, Bytes: 42, ContentType: "image/jpeg"}}
	pins, err := signingLineageStoragePins(who, ready, expected, body, "test-bucket")
	if err != nil {
		t.Fatal(err)
	}
	reserve := func(t *testing.T, p []byte, ref v2input.Ref) (string, uuid.UUID) {
		t.Helper()
		var outcome string
		var id, token *uuid.UUID
		var replay bool
		if err := tx.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v31_signing_lineage($1,$2,$3,$4,$5::jsonb,$6,$7,$8,'test-bucket')`, tid, user, session, export, string(p), ref.ArtifactID, ref.SHA256, int64(len(body))).Scan(&outcome, &id, &token, &replay); err != nil {
			t.Fatal(err)
		}
		if outcome == "created" || outcome == "resumed" || outcome == "replayed" {
			if id == nil || *id != ref.ArtifactID || token == nil || *token == uuid.Nil || replay != (outcome == "replayed") {
				t.Fatal("malformed SQL reservation")
			}
			return outcome, *token
		}
		if id != nil || token != nil || replay {
			t.Fatal("failed reservation leaked receipt")
		}
		return outcome, uuid.Nil
	}
	complete := func(t *testing.T, token uuid.UUID, p []byte, objectVersion string) bool {
		t.Helper()
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.complete_v31_signing_lineage($1,$2,$3,$4,$5::jsonb,$6,$7,$8,'test-bucket',$9,$10)`, tid, user, session, export, string(p), expected.ArtifactID, expected.SHA256, int64(len(body)), token, objectVersion).Scan(&accepted); err != nil {
			t.Fatal(err)
		}
		return accepted
	}
	usage := func(t *testing.T) (int64, int64) {
		t.Helper()
		var stored, reserved int64
		if err := tx.QueryRow(ctx, `SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1`, tid).Scan(&stored, &reserved); err != nil {
			t.Fatal(err)
		}
		return stored, reserved
	}
	for _, mode := range []string{"resume_fencing", "admission_ready", "complete_replay_gc", "quota", "changed_content", "changed_pins", "revoked_download", "revoked_session", "cleaning_refused", "cleaned_refused"} {
		t.Run(mode, func(t *testing.T) {
			exec(t, `SAVEPOINT lineage_case`)
			defer exec(t, `ROLLBACK TO SAVEPOINT lineage_case`)
			baseStored, baseReserved := usage(t)
			if mode == "quota" {
				exec(t, `UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}',to_jsonb($2::bigint)) WHERE id=$1`, tid, baseStored+baseReserved+int64(len(body))-1)
				outcome, _ := reserve(t, pins, expected)
				if outcome != "quota" {
					t.Fatal("quota bypass", outcome)
				}
				s, r := usage(t)
				if s != baseStored || r != baseReserved {
					t.Fatal("quota failure changed usage")
				}
				return
			}
			outcome, first := reserve(t, pins, expected)
			if outcome != "created" {
				t.Fatal("first reservation", outcome)
			}
			stored, reserved := usage(t)
			if stored != baseStored || reserved != baseReserved+int64(len(body)) {
				t.Fatal("incorrect initial reservation")
			}
			switch mode {
			case "resume_fencing":
				exec(t, `UPDATE photo.backend_documents SET cleanup_after=now()-interval '1 hour' WHERE artifact_id=$1`, expected.ArtifactID)
				outcome, next := reserve(t, pins, expected)
				if outcome != "resumed" || next == first {
					t.Fatal("resume did not rotate token")
				}
				var renewed bool
				scan(t, `SELECT cleanup_after>clock_timestamp()+interval '2 hours' FROM photo.backend_documents WHERE artifact_id=$1`, &renewed, expected.ArtifactID)
				if !renewed {
					t.Fatal("pending cleanup deadline not renewed")
				}
				if complete(t, first, pins, "lineage-v1") || !complete(t, next, pins, "lineage-v1") {
					t.Fatal("fencing failed")
				}
			case "admission_ready":
				if !complete(t, first, pins, "lineage-v1") {
					t.Fatal("lineage completion failed")
				}
				config, signParams, policyNow := signingRegistryFixture(t)
				installedPolicy := config.Policies[0]
				exec(t, `INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at,state) VALUES($1,$2,'test-bucket',$3,'policy-v1',$4,$5,'application/json',now(),'ready')`, installedPolicy.Ref.ArtifactID, tid, "policy/"+uuid.NewString(), installedPolicy.Ref.SHA256, int64(len(installedPolicy.Body)))
				signParams.Export = ready.Ref
				signParams.Lineage = expected
				paramsBody, err := canonical.Marshal(signParams)
				if err != nil {
					t.Fatal(err)
				}
				check := func() bool {
					var accepted bool
					scan(t, `SELECT photo.v31_signing_admission_ready($1,$2,$3,$4,$5::jsonb,'test-bucket',$6)`, &accepted, tid, user, session, export, string(pins), string(paramsBody))
					return accepted
				}
				if !check() {
					t.Fatal("exact ready admission rejected")
				}
				registry, err := NewSigningPolicyRegistry(config, &signingAuthorityStub{})
				if err != nil {
					t.Fatal(err)
				}
				source := &signingSourceStub{evidence: SigningExportEvidence{Project: project, ExportID: export, Export: ready.Ref, MIME: ready.Object.ContentType, Lineage: expected, LineageBody: body}}
				intent, err := PrepareSigningIntent(ctx, who, tid, export, signParams.PolicyID, "sql-draft", source, registry, policyNow)
				if err != nil {
					t.Fatal(err)
				}
				compiled, err := CompileSigningTask(ctx, intent, registry, ready.Object.ContentType, SigningTaskResources{ContractsSHA: strings.Repeat("d", 64), RAMBytes: 32 << 20, ScratchBytes: 1 << 30, OutputBytes: (512 << 20) + (16 << 20), MaxInputJSONBytes: 16 << 20}, policyNow)
				if err != nil {
					t.Fatal(err)
				}
				prepared := PreparedSigningActivation{intent: intent, compiled: compiled, mime: ready.Object.ContentType}
				exec(t, `SELECT set_config('app.tenant_id',$1,true)`, tid.String())
				if err := PrepareSigningDraft(ctx, tx, who, ready, prepared, "test-bucket"); err != nil {
					t.Fatal("draft creation", err)
				}
				if err := PrepareSigningDraft(ctx, tx, who, ready, prepared, "test-bucket"); err != nil {
					t.Fatal("draft replay", err)
				}
				var taskCount, itemCount int64
				scan(t, `SELECT count(*) FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2`, &taskCount, tid, intent.RunID())
				scan(t, `SELECT count(*) FROM photo.snapshot_items WHERE tenant_id=$1 AND snapshot_id=$2`, &itemCount, tid, uuid.NewSHA1(intent.WorkflowID(), []byte("provenance:artifact-snapshot:1")))
				if taskCount != 0 || itemCount != 3 {
					t.Fatal("draft dispatched task or duplicated snapshot")
				}
				paramsRef, paramsData := compiled.ParamsDocument()
				reserveParams := func() uuid.UUID {
					var outcome string
					var id, token *uuid.UUID
					var replay bool
					if err := tx.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v31_signing_params($1,$2,$3,$4,$5,$6,$7,$8,$9,'test-bucket')`, tid, user, session, intent.TaskID(), "sql-draft", intent.RequestSHA(), paramsRef.ArtifactID, paramsRef.SHA256, int64(len(paramsData))).Scan(&outcome, &id, &token, &replay); err != nil {
						t.Fatal(err)
					}
					if id == nil || *id != paramsRef.ArtifactID || token == nil || (outcome != "created" && outcome != "resumed") {
						t.Fatal("Params reservation failed", outcome)
					}
					return *token
				}
				firstParams, secondParams := reserveParams(), reserveParams()
				if firstParams == secondParams {
					t.Fatal("Params resume did not fence")
				}
				finishParams := func(token uuid.UUID) bool {
					var accepted bool
					scan(t, `SELECT photo.complete_v31_signing_params($1,$2,$3,$4,$5,$6,$7,$8,$9,'test-bucket',$10,'params-v1')`, &accepted, tid, user, session, intent.TaskID(), "sql-draft", intent.RequestSHA(), paramsRef.ArtifactID, paramsRef.SHA256, int64(len(paramsData)), token)
					return accepted
				}
				if finishParams(firstParams) || !finishParams(secondParams) || !finishParams(secondParams) {
					t.Fatal("Params completion fencing/replay failed")
				}
				exec(t, `UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','1073741824'::jsonb) WHERE id=$1`, tid)
				_, beforeMaterial := usage(t)
				if err := MaterializeSigningTask(ctx, tx, who, ready, prepared, "test-bucket"); err != nil {
					t.Fatal("blocked materialization", err)
				}
				if err := MaterializeSigningTask(ctx, tx, who, ready, prepared, "test-bucket"); err != nil {
					t.Fatal("blocked materialization replay", err)
				}
				_, afterMaterial := usage(t)
				if afterMaterial-beforeMaterial != (512<<20)+(16<<20) {
					t.Fatal("output quota reserved more than once")
				}
				var blocked bool
				scan(t, `SELECT state='blocked' AND attempt=0 AND fencing_token=0 AND inputs='[]'::jsonb AND canonical_result_id IS NULL FROM photo.tasks WHERE tenant_id=$1 AND id=$2`, &blocked, tid, intent.TaskID())
				if !blocked {
					t.Fatal("materialization exposed runnable task")
				}
				taskRaw, err := canonical.Marshal(compiled.Task())
				if err != nil {
					t.Fatal(err)
				}
				var transportReady bool
				scan(t, `SELECT photo.v31_signing_transport_ready($1,$2,$3,$4,$5,$6::jsonb)`, &transportReady, tid, user, session, "sql-draft", intent.RequestSHA(), string(taskRaw))
				if !transportReady {
					t.Fatal("materialized signer transport authority refused")
				}
				var genericSealed, activated bool
				scan(t, `SELECT photo.seal_v2_graph($1,$2)`, &genericSealed, tid, intent.RunID())
				if genericSealed {
					t.Fatal("generic signer activation bypass")
				}
				scan(t, `SELECT photo.activate_v31_signing($1,$2,$3,$4,$5,$6::jsonb,'{}'::jsonb)`, &activated, tid, user, session, "sql-draft", intent.RequestSHA(), string(taskRaw))
				if activated {
					t.Fatal("unregistered signer inputs activated")
				}
				if ticket, found, err := RecoverSigningTicket(ctx, tx, who, tid, export, signParams.PolicyID, "sql-draft"); err != nil || found || ticket.RunID != uuid.Nil {
					t.Fatal("unactivated draft recovered as ticket", err)
				}
				var paramsRetained bool
				scan(t, `SELECT photo.v2_artifact_has_references($1,$2)`, &paramsRetained, tid, paramsRef.ArtifactID)
				if !paramsRetained {
					t.Fatal("draft Params lost to GC")
				}

				exec(t, `UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE id=$1`, installedPolicy.Ref.ArtifactID)
				if check() {
					t.Fatal("expired policy admitted")
				}
			case "complete_replay_gc":
				if !complete(t, first, pins, "lineage-v1") || !complete(t, first, pins, "lineage-v1") || complete(t, first, pins, "different-version") {
					t.Fatal("completion replay failed")
				}
				outcome, replayToken := reserve(t, pins, expected)
				if outcome != "replayed" || replayToken != first {
					t.Fatal("ready replay changed identity")
				}
				s, r := usage(t)
				if s != baseStored+int64(len(body)) || r != baseReserved {
					t.Fatal("completion counted more than once")
				}
				var retained bool
				scan(t, `SELECT photo.v2_artifact_has_references($1,$2)`, &retained, tid, expected.ArtifactID)
				if !retained {
					t.Fatal("ready export lineage not retained")
				}
				exec(t, `UPDATE photo.exports SET status='queued' WHERE id=$1`, export)
				scan(t, `SELECT photo.v2_artifact_has_references($1,$2)`, &retained, tid, expected.ArtifactID)
				if retained {
					t.Fatal("unreferenced lineage retained after export retirement")
				}
			case "changed_content":
				altered := expected
				altered.SHA256 = strings.Repeat("c", 64)
				altered.ArtifactID = uuid.NewSHA1(export, []byte("signing:lineage:"+altered.SHA256))
				outcome, _ := reserve(t, pins, altered)
				if outcome != "conflict" {
					t.Fatal("changed lineage accepted", outcome)
				}
			case "changed_pins":
				var modified map[string]any
				if json.Unmarshal(pins, &modified) != nil {
					t.Fatal("invalid fixture pins")
				}
				modified["object_version"] = "replaced-source"
				different, _ := json.Marshal(modified)
				outcome, _ := reserve(t, different, expected)
				if outcome != "forbidden" || complete(t, first, different, "lineage-v1") {
					t.Fatal("changed source pins accepted")
				}
			case "revoked_download":
				exec(t, `UPDATE photo.projects SET download_allowed=false WHERE id=$1`, project)
				outcome, _ := reserve(t, pins, expected)
				if outcome != "forbidden" || complete(t, first, pins, "lineage-v1") {
					t.Fatal("revoked download accepted")
				}
			case "revoked_session":
				exec(t, `UPDATE photo.sessions SET expires_at=now()-interval '1 second' WHERE id=$1`, session)
				outcome, _ := reserve(t, pins, expected)
				if outcome != "forbidden" || complete(t, first, pins, "lineage-v1") {
					t.Fatal("expired session accepted")
				}
			case "cleaning_refused", "cleaned_refused":
				cleanupToken := uuid.New()
				exec(t, `UPDATE photo.backend_documents SET state='cleaning',cleanup_token=$2,cleanup_lease_until=now()+interval '1 minute' WHERE artifact_id=$1`, expected.ArtifactID, cleanupToken)
				if mode == "cleaned_refused" {
					var document uuid.UUID
					scan(t, `SELECT id FROM photo.backend_documents WHERE artifact_id=$1`, &document, expected.ArtifactID)
					var finished bool
					scan(t, `SELECT photo.finish_document_cleanup($1,$2)`, &finished, document, cleanupToken)
					if !finished {
						t.Fatal("acknowledged cleanup not completed")
					}
				}
				outcome, _ := reserve(t, pins, expected)
				if outcome != "stale" || complete(t, first, pins, "late-version") {
					t.Fatal("cleanup tombstone reopened")
				}
				s, r := usage(t)
				want := baseReserved + int64(len(body))
				if mode == "cleaned_refused" {
					want = baseReserved
				}
				if s != baseStored || r != want {
					t.Fatal("cleanup accounting changed on retry")
				}
			}
		})
	}
}

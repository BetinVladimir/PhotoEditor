package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/taskrecovery"
	"photoeditor/backend/internal/workflow"
	"strconv"
	"testing"
	"time"
)

// The caller owns the temporary database and versioned bucket. Native defect
// fixtures dispatch/claim a committed ready task before real worker execution.
func TestIntegrationV27IndexedDownloads(t *testing.T) {
	dsn, rawTask := os.Getenv("PHOTO_TEST_DATABASE_URL"), os.Getenv("PHOTO_TEST_V27_GATEWAY_TASK")
	if dsn == "" || rawTask == "" {
		t.Skip("isolated V27 database/task/bucket required")
	}
	task, err := uuid.Parse(rawTask)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	// The isolated fixture has a controlled imported qualification. Production
	// activation remains separate; restore its registry gate after this wire test.
	var operation string
	if err := admin.QueryRow(ctx, "SELECT operation FROM photo.tasks WHERE id=$1", task).Scan(&operation); err != nil {
		t.Fatal(err)
	}
	if !shownPreviewWireOperation(operation) && !shownCopyWireOperation(operation) && operation != "semantic.transfer" && operation != "v2.semantic.assemble" && operation != "semantic.local_exposure" && operation != "defects.propose" && operation != "mask.person" && operation != "face.detect" && operation != "face.landmarks" && operation != "face.parse" && operation != "mask.face_skin" && operation != "scene.parse" && operation != "depth.predict" {
		t.Fatal("unsupported fixture operation")
	}
	var enabled bool
	if err := admin.QueryRow(ctx, "SELECT v2_enabled FROM photo.operation_registry WHERE operation=$1 AND version='1'", operation).Scan(&enabled); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, "UPDATE photo.operation_registry SET v2_enabled=true WHERE operation=$1 AND version='1'", operation); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(context.Background(), "UPDATE photo.operation_registry SET v2_enabled=$1 WHERE operation=$2 AND version='1'", enabled, operation); err != nil {
			t.Error(err)
		}
	}()
	serverCert, clientCert, roots := wireCertificates(t)
	if operation == "semantic.transfer" && os.Getenv("PHOTO_TEST_V27_SCENE_TAIL") == "1" {
		claimNativeWireTask(t, ctx, admin, task, certificateHash(clientCert.Leaf), operation, "gpu", nativeQualifiedFixtureBackend())
	}
	if operation == "mask.person" || operation == "face.detect" || operation == "face.landmarks" || operation == "face.parse" || operation == "scene.parse" || operation == "depth.predict" {
		claimNativeWireTask(t, ctx, admin, task, certificateHash(clientCert.Leaf), operation, "gpu", nativeQualifiedFixtureBackend())
	}
	if operation == "mask.face_skin" {
		claimNativeWireTask(t, ctx, admin, task, certificateHash(clientCert.Leaf), operation, "cpu", "cpu")
	}
	if operation == "defects.propose" {
		claimNativeDefectWireTask(t, ctx, admin, task, certificateHash(clientCert.Leaf))
	}
	if operation == "semantic.preview_local_exposure" || shownCopyWireOperation(operation) {
		claimNativeWireTask(t, ctx, admin, task, certificateHash(clientCert.Leaf), operation, "cpu", "cpu")
	}
	if operation == "semantic.preview_remove_object" || operation == "semantic.preview_background_cleanup" {
		var backend string
		if err := admin.QueryRow(ctx, "SELECT execution_profile->>'backend' FROM photo.tasks WHERE id=$1", task).Scan(&backend); err != nil || backend != "cpu" {
			t.Fatal("controlled removal requires explicit frozen CPU backend", err)
		}
		claimNativeWireTask(t, ctx, admin, task, certificateHash(clientCert.Leaf), operation, "gpu", backend)
	}
	retrySuccess := probeV27RetryBeforeSuccess(t, ctx, admin, task, operation)
	if os.Getenv("PHOTO_TEST_V27_NATIVE_TERMINAL_WORKER_FAILURE") == "1" {
		probeV27WorkerFailure(t, ctx, admin, task)
		return
	}
	var id TaskIdentity
	err = admin.QueryRow(ctx, "SELECT tenant_id,run_id,id,worker_id,attempt,fencing_token FROM photo.tasks WHERE id=$1", task).Scan(&id.TenantID, &id.RunID, &id.TaskID, &id.WorkerID, &id.Attempt, &id.Fence)
	if err != nil {
		t.Fatal(err)
	}
	_, err = admin.Exec(ctx, "UPDATE photo.worker_registrations SET certificate_sha256=$1 WHERE worker_id=$2", certificateHash(clientCert.Leaf), id.WorkerID)
	if err != nil {
		t.Fatal(err)
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_gateway"
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	bucket := os.Getenv("PHOTO_TEST_MINIO_BUCKET")
	storage, err := objectstore.New(objectstore.Config{Endpoint: os.Getenv("PHOTO_TEST_MINIO_URL"), Bucket: bucket, AccessKey: os.Getenv("PHOTO_TEST_MINIO_ACCESS"), SecretKey: os.Getenv("PHOTO_TEST_MINIO_SECRET"), Development: true})
	if err != nil {
		t.Fatal(err)
	}
	signer, _, _, _ := fixture(t)
	signer.Now = time.Now
	token, err := signer.Issue(id, clientCert.Leaf, time.Now().Add(time.Minute), time.Now().Add(time.Hour), []string{ReadObjects})
	if err != nil {
		t.Fatal(err)
	}
	api := HTTP{Signer: signer, Tasks: Authorizer{DB: pool}, Inputs: Authorizer{DB: pool}, Objects: storage, Bucket: bucket, MaxDownloadBytes: 8 << 20, MaxUploadBytes: 825169920, Uploads: &UploadService{Repository: SQLOutputs{DB: pool, Bucket: bucket}, Objects: storage}}
	server := httptest.NewUnstartedServer(api.Handler())
	server.TLS = &tls.Config{Certificates: []tls.Certificate{serverCert}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert, MinVersion: tls.VersionTLS13}
	server.StartTLS()
	defer server.Close()
	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots, Certificates: []tls.Certificate{clientCert}, MinVersion: tls.VersionTLS13}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 10 * time.Second}
	download := func(artifact uuid.UUID, fence int64, status int) []byte {
		t.Helper()
		request, err := http.NewRequestWithContext(ctx, "GET", server.URL+"/internal/v1/tasks/"+task.String()+"/objects/"+artifact.String(), nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("X-Photo-Worker", id.WorkerID.String())
		request.Header.Set("X-Photo-Attempt", fmt.Sprint(id.Attempt))
		request.Header.Set("X-Photo-Fence", fmt.Sprint(fence))
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != status {
			var contract, state, version string
			var indexed bool
			diagnosticErr := admin.QueryRow(ctx, `SELECT coalesce(i.contract,''),a.state,coalesce(a.object_version,''),i.artifact_id IS NOT NULL FROM photo.artifacts a LEFT JOIN photo.v2_task_inputs i ON i.tenant_id=a.tenant_id AND i.artifact_id=a.id AND i.task_id=$1 WHERE a.tenant_id=$2 AND a.id=$3`, task, id.TenantID, artifact).Scan(&contract, &state, &version, &indexed)
			var authorizedRows int
			gatewayErr := pool.QueryRow(ctx, `SELECT count(*) FROM photo.gateway_input($1,$2,$3,$4,$5,$6)`, task, id.Attempt, id.Fence, id.WorkerID, certificateHash(clientCert.Leaf), artifact).Scan(&authorizedRows)
			t.Fatalf("download status=%d expected=%d artifact=%s contract=%s indexed=%t state=%s versionPresent=%t diagnostic=%v gatewayRows=%d gatewayError=%v", response.StatusCode, status, artifact, contract, indexed, state, version != "", diagnosticErr, authorizedRows, gatewayErr)
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
		if err != nil {
			t.Fatal(err)
		}
		if status == 200 {
			size, err := strconv.ParseInt(response.Header.Get("Content-Length"), 10, 64)
			if err != nil || size != int64(len(body)) {
				t.Fatal("content length mismatch")
			}
		}
		return body
	}
	var closureReady bool
	var entryCount int
	if err := admin.QueryRow(ctx, "SELECT photo.v2_input_closure_ready(id),(execution_profile->>'v2_entry_count')::integer FROM photo.tasks WHERE tenant_id=$1 AND id=$2", id.TenantID, task).Scan(&closureReady, &entryCount); err != nil {
		t.Fatal(err)
	}
	// Apply children use their sealed approved-semantic inventory rather than
	// a v2_workflows row; the Python exposure installer proves leased admission.
	if (!closureReady && operation != "semantic.local_exposure" && !shownPreviewWireOperation(operation) && !shownCopyWireOperation(operation)) || entryCount < 1 {
		t.Fatal("incomplete authenticated input closure")
	}

	if shownPreviewWireOperation(operation) || shownCopyWireOperation(operation) {
		var probe uuid.UUID
		if err := admin.QueryRow(ctx, "SELECT artifact_id FROM photo.v2_task_inputs WHERE tenant_id=$1 AND task_id=$2 AND contract='InputClosureChunk'", id.TenantID, task).Scan(&probe); err != nil {
			t.Fatal(err)
		}
		var direct uuid.UUID
		if err := admin.QueryRow(ctx, "SELECT artifact_id FROM photo.v2_task_inputs WHERE tenant_id=$1 AND task_id=$2 AND contract='Params'", id.TenantID, task).Scan(&direct); err != nil {
			t.Fatal(err)
		}
		download(direct, id.Fence, 200)
		download(probe, id.Fence, 200)
		func() {
			if _, err := admin.Exec(ctx, "UPDATE photo.operation_registry SET v2_enabled=false WHERE operation=$1 AND version='1'", operation); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := admin.Exec(context.Background(), "UPDATE photo.operation_registry SET v2_enabled=true WHERE operation=$1 AND version='1'", operation); err != nil {
					t.Error(err)
				}
			}()
			download(direct, id.Fence, 403)
			download(probe, id.Fence, 403)
		}()
		download(probe, id.Fence, 200)
		var project uuid.UUID
		if err := admin.QueryRow(ctx, "SELECT project_id FROM photo.v2_task_inputs WHERE tenant_id=$1 AND task_id=$2 AND artifact_id=$3", id.TenantID, task, probe).Scan(&project); err != nil {
			t.Fatal(err)
		}
		func() {
			if _, err := admin.Exec(ctx, "UPDATE photo.projects SET archived_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2", id.TenantID, project); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := admin.Exec(context.Background(), "UPDATE photo.projects SET archived_at=NULL WHERE tenant_id=$1 AND id=$2", id.TenantID, project); err != nil {
					t.Error(err)
				}
			}()
			download(direct, id.Fence, 403)
			download(probe, id.Fence, 403)
		}()
		download(direct, id.Fence, 200)
		download(probe, id.Fence, 200)
		var reservation uuid.UUID
		var expiry time.Time
		if err := admin.QueryRow(ctx, "SELECT id,expires_at FROM photo.quota_reservations WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2 AND state='reserved'", id.TenantID, id.RunID).Scan(&reservation, &expiry); err != nil {
			t.Fatal(err)
		}
		func() {
			if _, err := admin.Exec(ctx, "UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND id=$2", id.TenantID, reservation); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if _, err := admin.Exec(context.Background(), "UPDATE photo.quota_reservations SET expires_at=$1 WHERE tenant_id=$2 AND id=$3", expiry, id.TenantID, reservation); err != nil {
					t.Error(err)
				}
			}()
			download(direct, id.Fence, 403)
			download(probe, id.Fence, 403)
		}()
		download(direct, id.Fence, 200)
		download(probe, id.Fence, 200)
	}
	rows, err := admin.Query(ctx, "SELECT artifact_id,sha256 FROM photo.v2_task_inputs WHERE tenant_id=$1 AND task_id=$2 ORDER BY ordinal", id.TenantID, task)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var artifact uuid.UUID
		var sha string
		if err := rows.Scan(&artifact, &sha); err != nil {
			t.Fatal(err)
		}
		if operation == "semantic.local_exposure" && count == 0 {
			var project uuid.UUID
			if err := admin.QueryRow(ctx, "SELECT project_id FROM photo.v2_task_inputs WHERE tenant_id=$1 AND task_id=$2 AND artifact_id=$3", id.TenantID, task, artifact).Scan(&project); err != nil {
				t.Fatal(err)
			}
			if _, err := admin.Exec(ctx, "UPDATE photo.projects SET archived_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2", id.TenantID, project); err != nil {
				t.Fatal(err)
			}
			func() {
				defer func() {
					if _, err := admin.Exec(context.Background(), "UPDATE photo.projects SET archived_at=NULL WHERE tenant_id=$1 AND id=$2", id.TenantID, project); err != nil {
						t.Error(err)
					}
				}()
				download(artifact, id.Fence, 404)
			}()
		}
		body := download(artifact, id.Fence, 200)
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != sha {
			t.Fatal("pinned digest mismatch")
		}
		count++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if expected := entryCount + (entryCount+511)/512 + 1; count != expected {
		t.Fatalf("physical input count=%d expected=%d", count, expected)
	}
	download(uuid.New(), id.Fence, 404)
	download(task, id.Fence+1, 403)
	root := os.Getenv("PHOTO_TEST_V27_REPO_ROOT")
	if root == "" {
		t.Fatal("owned repository root required")
	}
	parent := filepath.Join(root, ".runtime", "v27-worker-wire")
	if err := os.MkdirAll(parent, 0700); err != nil {
		t.Fatal(err)
	}
	// ScratchSpace forbids lexical symlink ancestors. Resolve only the owned
	// infrastructure parent; source/module ROOT remains the composed source view.
	parent, err = filepath.EvalSymlinks(parent)
	if err != nil {
		t.Fatal(err)
	}
	directory, err := os.MkdirTemp(parent, "probe-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(directory)
	key, err := x509.MarshalPKCS8PrivateKey(clientCert.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"ca.pem":     pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientCert.Certificate[1]}),
		"client.pem": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientCert.Certificate[0]}),
		"client.key": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(directory, name), body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	probeFile := "v27_worker_wire_probe.py"
	if operation == "semantic.transfer" && os.Getenv("PHOTO_TEST_V27_SCENE_TAIL") == "1" {
		probeFile = "v27_scene_transfer_wire_probe.py"
	}
	if operation == "mask.person" {
		probeFile = "v27_person_wire_probe.py"
	}
	if operation == "face.detect" || operation == "face.landmarks" || operation == "face.parse" || operation == "mask.face_skin" {
		probeFile = "v27_face_wire_probe.py"
	}
	if operation == "defects.propose" {
		probeFile = "v27_defect_wire_probe.py"
	}
	if operation == "v2.semantic.assemble" {
		probeFile = "v27_cpu_wire_probe.py"
	}
	if operation == "semantic.local_exposure" {
		probeFile = "v27_exposure_wire_probe.py"
	}
	if operation == "scene.parse" || operation == "depth.predict" {
		probeFile = "v27_scene_wire_probe.py"
	}
	if shownPreviewWireOperation(operation) || shownCopyWireOperation(operation) {
		probeFile = "v27_shown_preview_wire_probe.py"
	}
	probe := exec.CommandContext(ctx, filepath.Join(root, ".venv-platform", "bin", "python"), filepath.Join(root, "tests", "integration", probeFile))
	probe.Dir = root
	probe.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(root, "workers")+":"+filepath.Join(root, "apps", "local-editor", "src"), "PHOTO_TEST_V27_CERT_DIR="+directory, "PHOTO_TEST_V27_GATEWAY_ORIGIN="+server.URL)
	output, err := probe.CombinedOutput()
	if err != nil {
		t.Fatalf("Python worker probe failed: %v\n%s", err, output)
	}
	if !bytes.Contains(output, []byte("V27_WORKER_WIRE_OK")) {
		t.Fatal("worker probe did not confirm execution")
	}
	if shownCopyWireOperation(operation) {
		finalizeShownWireTask(t, ctx, admin, dsn, id)
	}
	if shownPreviewWireOperation(operation) {
		receiptColumn := "v27_preapply_preview_receipt"
		if operation == "semantic.preview_remove_object" {
			receiptColumn = "v27_removal_preview_receipt"
		} else if operation == "semantic.preview_background_cleanup" {
			receiptColumn = "v27_background_cleanup_preview_receipt"
		}
		var run uuid.UUID
		var before string
		var receiptAbsent bool
		if err := admin.QueryRow(ctx, "SELECT r.id,q.state,r."+receiptColumn+" IS NULL FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id WHERE t.id=$1", task).Scan(&run, &before, &receiptAbsent); err != nil || before != "reserved" || !receiptAbsent {
			t.Fatalf("preview execution prematurely settled quota or sealed receipt: state=%s absent=%v err=%v", before, receiptAbsent, err)
		}
		scheduler, err := admin.Acquire(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer scheduler.Release()
		if _, err := scheduler.Exec(ctx, "SET ROLE photo_scheduler"); err != nil {
			t.Fatal(err)
		}
		defer scheduler.Exec(context.Background(), "RESET ROLE")
		processor := workflow.RenderDerivativeCleanup{DB: scheduler}
		if changed, err := processor.ProcessOne(ctx); err != nil || !changed {
			t.Fatalf("mounted preview lifecycle: changed=%v err=%v", changed, err)
		}
		var status, quota string
		var unsettled int
		if err := admin.QueryRow(ctx, "SELECT r.status,q.state,r."+receiptColumn+" IS NULL,(SELECT count(*) FROM photo.output_uploads u WHERE u.tenant_id=r.tenant_id AND u.task_id=$2 AND u.state NOT IN('ready','cleaned')) FROM photo.runs r JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id WHERE r.id=$1", run, task).Scan(&status, &quota, &receiptAbsent, &unsettled); err != nil || status != "completed" || quota != "released" || !receiptAbsent || unsettled != 0 {
			t.Fatalf("preview lifecycle state=%s quota=%s receiptAbsent=%v unsettled=%d err=%v", status, quota, receiptAbsent, unsettled, err)
		}
	}
	t.Log("V27_WORKER_WIRE_OK")
	if shownPreviewWireOperation(operation) || shownCopyWireOperation(operation) {
		t.Log("V27_SHOWN_WORKER_WIRE_OK")
	}
	if retrySuccess {
		t.Log("V27_TARGET_WORKER_RETRY_SUCCESS_OK")
	}
	if operation == "v2.semantic.assemble" {
		download(uuid.New(), id.Fence, 403)
	}
}

func finalizeShownWireTask(t *testing.T, ctx context.Context, admin *pgxpool.Pool, dsn string, id TaskIdentity) {
	t.Helper()
	// A real nonprivileged session identity, not an administrator SET ROLE pool.
	login := "wire_scheduler_" + uuid.New().String()
	password := uuid.New().String() + uuid.New().String()
	quoted := pgx.Identifier{login}.Sanitize()
	// UUID-derived password contains no SQL quotation characters. Never log it.
	if _, err := admin.Exec(ctx, "CREATE ROLE "+quoted+" LOGIN NOSUPERUSER NOBYPASSRLS NOCREATEROLE NOCREATEDB NOREPLICATION PASSWORD '"+password+"'"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, "DROP ROLE "+quoted); err != nil {
			t.Errorf("owned scheduler login cleanup failed: %v", err)
		}
	}()
	if _, err := admin.Exec(ctx, "GRANT photo_scheduler TO "+quoted); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.User, cfg.ConnConfig.Password = login, password
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	cfg.MaxConns = 1
	scheduler, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer scheduler.Close() // LIFO: close all owned sessions before DROP ROLE.
	var session, role string
	if err := scheduler.QueryRow(ctx, "SELECT session_user,current_user").Scan(&session, &role); err != nil || session != login || role != "photo_scheduler" {
		t.Fatalf("scheduler identity mismatch: session=%s role=%s err=%v", session, role, err)
	}
	// Snapshot exact immutable parent state, not unrelated scheduler work counts.
	const parentQuery = `SELECT jsonb_build_object('run',to_jsonb(parent),'batch',to_jsonb(batch))::text
 FROM photo.semantic_targets target JOIN photo.semantic_intents intent ON intent.tenant_id=target.tenant_id AND intent.id=target.intent_id
 JOIN photo.v2_workflows workflow ON workflow.tenant_id=intent.tenant_id AND workflow.id=intent.workflow_id
 JOIN photo.runs parent ON parent.tenant_id=workflow.tenant_id AND parent.id=workflow.run_id
 JOIN photo.v2_review_batches batch ON batch.tenant_id=target.tenant_id AND batch.id=target.batch_id
 WHERE target.tenant_id=$1 AND target.child_run_id=$2`
	var parentBefore string
	if err := admin.QueryRow(ctx, parentQuery, id.TenantID, id.RunID).Scan(&parentBefore); err != nil {
		t.Fatal(err)
	}
	const stateQuery = `SELECT jsonb_build_object('run',to_jsonb(r),'targets',(SELECT jsonb_agg(to_jsonb(target) ORDER BY target.id) FROM photo.semantic_targets target WHERE target.tenant_id=r.tenant_id AND target.child_run_id=r.id),
 'quota',(SELECT jsonb_agg(to_jsonb(q) ORDER BY q.id) FROM photo.quota_reservations q WHERE q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id),
 'task_results',(SELECT jsonb_agg(jsonb_build_object('task',to_jsonb(t),'result',to_jsonb(result),'inbox',to_jsonb(i)) ORDER BY t.id) FROM photo.tasks t JOIN photo.task_results result ON result.tenant_id=t.tenant_id AND result.id=t.canonical_result_id JOIN photo.inbox i ON i.tenant_id=t.tenant_id AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id) WHERE t.tenant_id=r.tenant_id AND t.run_id=r.id),
 'usage',(SELECT to_jsonb(u) FROM photo.tenant_usage u WHERE u.tenant_id=r.tenant_id),
 'events',(SELECT jsonb_agg(to_jsonb(e) ORDER BY e.cursor) FROM photo.run_events e WHERE e.tenant_id=r.tenant_id AND e.run_id=r.id))::text
 FROM photo.runs r WHERE r.tenant_id=$1 AND r.id=$2`
	var initial string
	if err := admin.QueryRow(ctx, stateQuery, id.TenantID, id.RunID).Scan(&initial); err != nil {
		t.Fatal(err)
	}
	var ready bool
	if err := admin.QueryRow(ctx, `SELECT r.status='running' AND t.state='succeeded' AND t.canonical_result_id IS NOT NULL
 AND EXISTS(SELECT 1 FROM photo.inbox i WHERE i.tenant_id=t.tenant_id AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id))
 AND NOT EXISTS(SELECT 1 FROM photo.run_events e WHERE e.tenant_id=r.tenant_id AND e.run_id=r.id AND e.type='semantic.child.completed')
 FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE t.id=$1`, id.TaskID).Scan(&ready); err != nil || !ready {
		t.Fatalf("shown pre-finalization invariant: ready=%v err=%v", ready, err)
	}
	var quotaID uuid.UUID
	var remainingBefore, reservedBefore, storageBefore int64
	if err := admin.QueryRow(ctx, `SELECT q.id,q.bytes,u.reserved_bytes,u.storage_bytes FROM photo.quota_reservations q JOIN photo.tenant_usage u ON u.tenant_id=q.tenant_id WHERE q.tenant_id=$1 AND q.holder_type='run' AND q.holder_id=$2 AND q.state='reserved'`, id.TenantID, id.RunID).Scan(&quotaID, &remainingBefore, &reservedBefore, &storageBefore); err != nil {
		t.Fatal(err)
	}
	const canonicalQuery = `SELECT jsonb_agg(jsonb_build_object('task',to_jsonb(t),'result',to_jsonb(result),'inbox',to_jsonb(i)) ORDER BY t.id)::text FROM photo.tasks t JOIN photo.task_results result ON result.tenant_id=t.tenant_id AND result.id=t.canonical_result_id JOIN photo.inbox i ON i.tenant_id=t.tenant_id AND i.consumer='v2-adopt' AND i.event_id=photo.v2_adoption_event_id(t.id,t.canonical_result_id) WHERE t.tenant_id=$1 AND t.run_id=$2`
	var canonicalBefore, canonicalAfter string
	if err := admin.QueryRow(ctx, canonicalQuery, id.TenantID, id.RunID).Scan(&canonicalBefore); err != nil {
		t.Fatal(err)
	}
	recovery := taskrecovery.Recovery{DB: scheduler}
	if _, err := recovery.Recover(ctx, 10); err != nil {
		t.Fatal(err)
	}
	var completed bool
	if err := admin.QueryRow(ctx, `SELECT r.status='completed'
 AND EXISTS(SELECT 1 FROM photo.semantic_targets target WHERE target.tenant_id=r.tenant_id AND target.child_run_id=r.id)
 AND NOT EXISTS(SELECT 1 FROM photo.semantic_targets target WHERE target.tenant_id=r.tenant_id AND target.child_run_id=r.id AND target.state<>'applied')
 AND EXISTS(SELECT 1 FROM photo.quota_reservations q JOIN photo.tenant_usage u ON u.tenant_id=q.tenant_id WHERE q.tenant_id=r.tenant_id AND q.id=$3 AND q.holder_type='run' AND q.holder_id=r.id AND q.state='consumed' AND q.bytes=0 AND u.reserved_bytes=$4::bigint-$5::bigint AND u.storage_bytes=$6::bigint)
 AND (SELECT count(*) FROM photo.run_events e WHERE e.tenant_id=r.tenant_id AND e.run_id=r.id AND e.type='semantic.child.completed')=1
 FROM photo.runs r WHERE r.tenant_id=$1 AND r.id=$2`, id.TenantID, id.RunID, quotaID, reservedBefore, remainingBefore, storageBefore).Scan(&completed); err != nil || !completed {
		t.Fatalf("shown mounted finalization invariant: completed=%v err=%v", completed, err)
	}
	if err := admin.QueryRow(ctx, canonicalQuery, id.TenantID, id.RunID).Scan(&canonicalAfter); err != nil || canonicalAfter != canonicalBefore {
		t.Fatalf("canonical task/result/inbox changed during finalization: %v", err)
	}
	var parentAfter, settled, replay string
	if err := admin.QueryRow(ctx, parentQuery, id.TenantID, id.RunID).Scan(&parentAfter); err != nil || parentAfter != parentBefore {
		t.Fatalf("parent review changed during child settlement: %v", err)
	}
	if err := admin.QueryRow(ctx, stateQuery, id.TenantID, id.RunID).Scan(&settled); err != nil {
		t.Fatal(err)
	}
	if settled == initial {
		t.Fatal("shown lifecycle did not progress")
	}
	if _, err := recovery.Recover(ctx, 10); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, stateQuery, id.TenantID, id.RunID).Scan(&replay); err != nil || replay != settled {
		t.Fatalf("shown lifecycle replay changed settled state: %v", err)
	}
	if err := admin.QueryRow(ctx, parentQuery, id.TenantID, id.RunID).Scan(&parentAfter); err != nil || parentAfter != parentBefore {
		t.Fatalf("parent review changed on recovery replay: %v", err)
	}
}

// This is fixture dispatch only; it cannot enable an unknown operation.
func shownCopyWireOperation(operation string) bool {
	switch operation {
	case "semantic.adopt_shown_exposure", "semantic.adopt_shown_retouch_region", "semantic.adopt_shown_removal", "semantic.adopt_shown_background_cleanup":
		return true
	default:
		return false
	}
}

func TestShownCopyWireOperationScope(t *testing.T) {
	for _, operation := range []string{"semantic.adopt_shown_exposure", "semantic.adopt_shown_retouch_region", "semantic.adopt_shown_removal", "semantic.adopt_shown_background_cleanup"} {
		if !shownCopyWireOperation(operation) {
			t.Errorf("known shown operation rejected: %s", operation)
		}
	}
	for _, operation := range []string{"", "semantic.preview_retouch_region", "semantic.adopt_shown_retouch_region.extra", "SEMANTIC.ADOPT_SHOWN_RETOUCH_REGION"} {
		if shownCopyWireOperation(operation) {
			t.Errorf("unknown/non-copy operation accepted: %s", operation)
		}
	}
}

func shownPreviewWireOperation(operation string) bool {
	switch operation {
	case "semantic.preview_local_exposure", "semantic.preview_remove_object", "semantic.preview_background_cleanup":
		return true
	default:
		return false
	}
}
func TestShownPreviewWireOperationScope(t *testing.T) {
	for _, op := range []string{"semantic.preview_local_exposure", "semantic.preview_remove_object", "semantic.preview_background_cleanup"} {
		if !shownPreviewWireOperation(op) {
			t.Fatal(op)
		}
	}
	for _, op := range []string{"", "semantic.preview_remove_object.extra", "semantic.adopt_shown_removal", "semantic.preview_background_cleanup.extra"} {
		if shownPreviewWireOperation(op) {
			t.Fatal(op)
		}
	}
}

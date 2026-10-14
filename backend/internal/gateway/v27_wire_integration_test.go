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
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"photoeditor/backend/internal/objectstore"
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
	if operation != "semantic.transfer" && operation != "v2.semantic.assemble" && operation != "semantic.local_exposure" && operation != "defects.propose" && operation != "mask.person" && operation != "face.detect" && operation != "face.landmarks" && operation != "face.parse" && operation != "mask.face_skin" {
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
	if operation == "mask.person" || operation == "face.detect" || operation == "face.landmarks" || operation == "face.parse" {
		claimNativeWireTask(t, ctx, admin, task, certificateHash(clientCert.Leaf), operation, "gpu", "cuda")
	}
	if operation == "mask.face_skin" {
		claimNativeWireTask(t, ctx, admin, task, certificateHash(clientCert.Leaf), operation, "cpu", "cpu")
	}
	if operation == "defects.propose" {
		claimNativeDefectWireTask(t, ctx, admin, task, certificateHash(clientCert.Leaf))
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
			t.Fatalf("download status=%d expected=%d", response.StatusCode, status)
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
	if (!closureReady && operation != "semantic.local_exposure") || entryCount < 1 {
		t.Fatal("incomplete authenticated input closure")
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
	probe := exec.CommandContext(ctx, filepath.Join(root, ".venv-platform", "bin", "python"), filepath.Join(root, "tests", "integration", probeFile))
	probe.Dir = root
	probe.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(root, "workers")+":"+filepath.Join(root, "src"), "PHOTO_TEST_V27_CERT_DIR="+directory, "PHOTO_TEST_V27_GATEWAY_ORIGIN="+server.URL)
	output, err := probe.CombinedOutput()
	if err != nil {
		t.Fatalf("Python worker probe failed: %v\n%s", err, output)
	}
	if !bytes.Contains(output, []byte("V27_WORKER_WIRE_OK")) {
		t.Fatal("worker probe did not confirm execution")
	}
	t.Log("V27_WORKER_WIRE_OK")
	if retrySuccess {
		t.Log("V27_TARGET_WORKER_RETRY_SUCCESS_OK")
	}
	if operation == "v2.semantic.assemble" {
		download(uuid.New(), id.Fence, 403)
	}
}

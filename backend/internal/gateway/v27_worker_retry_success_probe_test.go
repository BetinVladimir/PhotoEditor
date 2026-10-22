package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/workflow"
)

// Return with an actually re-admitted/claimed attempt2. The normal mTLS worker
// execution/adoption path then runs without a forged result or success state.
func probeV27RetryBeforeSuccess(t *testing.T, ctx context.Context, admin *pgxpool.Pool, task uuid.UUID, operation string) bool {
	t.Helper()
	if os.Getenv("PHOTO_TEST_V27_TARGET_WORKER_RETRY_SUCCESS_OPERATION") != operation {
		return false
	}
	key := map[string]string{"mask.person": "v27_target_person_prepare", "face.detect": "v27_target_detector_prepare", "face.landmarks": "v27_target_face_prepare", "face.parse": "v27_target_face_prepare", "mask.face_skin": "v27_target_skin_prepare", "defects.propose": "v27_target_defect_prepare"}[operation]
	var target bool
	if err := admin.QueryRow(ctx, "SELECT r.config_snapshot ? $2 FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE t.id=$1", task, key).Scan(&target); err != nil {
		t.Fatal(err)
	}
	if !target {
		return false
	}
	if os.Getenv("PHOTO_TEST_V27_NATIVE_TERMINAL_WORKER_FAILURE") == "1" {
		t.Fatal("conflicting success/failure probes")
	}
	var old struct {
		Task    string `json:"task"`
		Worker  string `json:"worker"`
		Attempt int    `json:"attempt"`
		Fence   int64  `json:"fence"`
	}
	var class, backend, before string
	if err := admin.QueryRow(ctx, "SELECT t.id::text,t.worker_id::text,t.attempt,t.fencing_token,t.resource_class,t.execution_profile->>'backend',jsonb_build_object('config',r.config_snapshot,'hash',r.config_hash,'params',t.params_artifact_id,'digest',t.input_digest,'profile',t.execution_profile,'class',t.resource_class)::text FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE t.id=$1", task).Scan(&old.Task, &old.Worker, &old.Attempt, &old.Fence, &class, &backend, &before); err != nil || old.Attempt != 1 {
		t.Fatal("first successful retry claim", err)
	}
	root := os.Getenv("PHOTO_TEST_V27_REPO_ROOT")
	if root == "" {
		t.Fatal("worker root required")
	}
	probe := func(staleOnly bool, identity string) {
		t.Helper()
		mode := "0"
		if staleOnly {
			mode = "1"
		}
		cmd := exec.CommandContext(ctx, filepath.Join(root, ".venv-platform/bin/python"), filepath.Join(root, "tests/integration/v27_worker_failure_probe.py"))
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(root, "workers")+":"+filepath.Join(root, "apps", "local-editor", "src"), "PHOTO_TEST_V27_FAILURE_RETRYABLE=1", "PHOTO_TEST_V27_OLD_FAILURE_IDENTITY="+identity, "PHOTO_TEST_V27_FAILURE_STALE_ONLY="+mode)
		out, err := cmd.CombinedOutput()
		marker := "V27_WORKER_FAILURE_COMMITTED_OK"
		if staleOnly {
			marker = "V27_WORKER_RETRY_SUCCESS_READY_OK"
		}
		if err != nil || !bytes.Contains(out, []byte(marker)) {
			t.Fatal("retry-success worker protocol", err, string(out))
		}
	}
	probe(false, "")
	assertV27RetryObserverHealthy(t, ctx, admin, task, operation, false)
	cfg := admin.Config().Copy()
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	scheduler, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer scheduler.Close()
	recovery := workflow.Recovery{DB: scheduler}
	if n, err := recovery.Recover(ctx, 10); err != nil || n < 1 {
		t.Fatal("success retry schedule", n, err)
	}
	var next *time.Time
	var state string
	var retries int
	if err := admin.QueryRow(ctx, "SELECT state,retry_count,next_attempt_at FROM photo.tasks WHERE id=$1", task).Scan(&state, &retries, &next); err != nil || state != "retry_wait" || retries != 1 || next == nil {
		t.Fatal("success retry pending", state, retries, err)
	}
	assertV27RetryObserverHealthy(t, ctx, admin, task, operation, false)
	delay := time.Until(*next) + 20*time.Millisecond
	if delay > 7*time.Second {
		t.Fatal("unexpected success retry delay", delay)
	}
	if delay > 0 {
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if n, err := recovery.Recover(ctx, 10); err != nil || n < 1 {
		t.Fatal("success retry activation", n, err)
	}
	if err := admin.QueryRow(ctx, "SELECT state FROM photo.tasks WHERE id=$1", task).Scan(&state); err != nil || state != "ready" {
		t.Fatal("success retry not ready", state, err)
	}
	assertV27RetryObserverHealthy(t, ctx, admin, task, operation, false)
	_, cert, _ := wireCertificates(t)
	claimNativeWireTask(t, ctx, admin, task, certificateHash(cert.Leaf), operation, class, backend)
	raw, err := json.Marshal(old)
	if err != nil {
		t.Fatal(err)
	}
	probe(true, string(raw))
	var after string
	if err := admin.QueryRow(ctx, "SELECT jsonb_build_object('config',r.config_snapshot,'hash',r.config_hash,'params',t.params_artifact_id,'digest',t.input_digest,'profile',t.execution_profile,'class',t.resource_class)::text FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE t.id=$1", task).Scan(&after); err != nil || after != before {
		t.Fatal("success retry changed frozen inputs/profile", err)
	}
	return true
}

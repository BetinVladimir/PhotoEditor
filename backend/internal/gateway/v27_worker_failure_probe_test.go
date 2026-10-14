package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"os/exec"
	"path/filepath"
	"photoeditor/backend/internal/workflow"
	"testing"
	"time"
)

// The native wire entry already performed real reservation/dispatch/claim.
// Python commits a nonretryable failure through the production worker DB API.
func probeV27WorkerFailure(t *testing.T, ctx context.Context, admin *pgxpool.Pool, task uuid.UUID) {
	t.Helper()
	root := os.Getenv("PHOTO_TEST_V27_REPO_ROOT")
	if root == "" {
		t.Fatal("owned worker failure root required")
	}
	retry := os.Getenv("PHOTO_TEST_V27_TARGET_WORKER_RETRY_ONCE") == "1"
	oom := os.Getenv("PHOTO_TEST_V27_FAILURE_CODE") == "RESOURCE_OOM"
	if oom && (retry || os.Getenv("PHOTO_TEST_V27_TARGET_WORKER_RETRY_EXHAUST") == "1") {
		t.Fatal("OOM refusal probe cannot use ordinary retry policy hooks")
	}
	runProbe := func(retryable bool, old string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, filepath.Join(root, ".venv-platform", "bin", "python"), filepath.Join(root, "tests", "integration", "v27_worker_failure_probe.py"))
		cmd.Dir = root
		value := "0"
		if retryable {
			value = "1"
		}
		cmd.Env = append(os.Environ(), "PYTHONPATH="+filepath.Join(root, "workers")+":"+filepath.Join(root, "src"), "PHOTO_TEST_V27_FAILURE_RETRYABLE="+value, "PHOTO_TEST_V27_OLD_FAILURE_IDENTITY="+old)
		out, err := cmd.CombinedOutput()
		if err != nil || !bytes.Contains(out, []byte("V27_WORKER_FAILURE_COMMITTED_OK")) {
			t.Fatal("actual worker failure commit", err, string(out))
		}
		if old != "" && !bytes.Contains(out, []byte("V27_WORKER_OLD_ATTEMPT_STALE_OK")) {
			t.Fatal("old retry identity not tested")
		}
	}
	var old struct {
		Task    string `json:"task"`
		Worker  string `json:"worker"`
		Attempt int    `json:"attempt"`
		Fence   int64  `json:"fence"`
	}
	if err := admin.QueryRow(ctx, "SELECT id::text,worker_id::text,attempt,fencing_token FROM photo.tasks WHERE id=$1", task).Scan(&old.Task, &old.Worker, &old.Attempt, &old.Fence); err != nil {
		t.Fatal(err)
	}
	fingerprint := func() string {
		t.Helper()
		var value string
		if err := admin.QueryRow(ctx, "SELECT jsonb_build_object('config',r.config_snapshot,'hash',r.config_hash,'params',t.params_artifact_id,'digest',t.input_digest,'profile',t.execution_profile,'class',t.resource_class)::text FROM photo.tasks t JOIN photo.runs r ON r.tenant_id=t.tenant_id AND r.id=t.run_id WHERE t.id=$1", task).Scan(&value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	before := fingerprint()
	if oom {
		var raw []byte
		if err := admin.QueryRow(ctx, "SELECT execution_profile FROM photo.tasks WHERE id=$1", task).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var profile map[string]json.RawMessage
		if err := json.Unmarshal(raw, &profile); err != nil {
			t.Fatal(err)
		}
		if _, ok := workflow.ReduceExecutionProfile(profile); ok {
			t.Fatal("unexpected qualified reduction in frozen native profile")
		}
		if _, ok := workflow.CPUFallbackProfile(profile); ok {
			t.Fatal("unexpected qualified CPU fallback in frozen native profile")
		}
	}
	cfg := admin.Config().Copy()
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	scheduler, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer scheduler.Close()
	if os.Getenv("PHOTO_TEST_V27_TARGET_WORKER_RETRY_EXHAUST") == "1" {
		// Exercise real failure/admission/claim transitions. Only the scheduler's
		// injectable policy clock is accelerated; no persisted due/state is patched.
		previous := ""
		for attempt := 1; attempt <= 4; attempt++ {
			if err := admin.QueryRow(ctx, "SELECT id::text,worker_id::text,attempt,fencing_token FROM photo.tasks WHERE id=$1", task).Scan(&old.Task, &old.Worker, &old.Attempt, &old.Fence); err != nil || old.Attempt != attempt {
				t.Fatal("budget claim identity", attempt, err)
			}
			runProbe(true, previous)
			var operation, class, backend string
			if err := admin.QueryRow(ctx, "SELECT operation,resource_class,execution_profile->>'backend' FROM photo.tasks WHERE id=$1", task).Scan(&operation, &class, &backend); err != nil {
				t.Fatal(err)
			}
			assertV27RetryObserverHealthy(t, ctx, admin, task, operation, false)
			recovery := workflow.Recovery{DB: scheduler, Now: func() time.Time { return time.Now().UTC().Add(-3 * time.Minute) }}
			if n, err := recovery.Recover(ctx, 10); err != nil || n < 1 {
				t.Fatal("budget recovery", attempt, n, err)
			}
			var state string
			var retries int
			var next *time.Time
			if err := admin.QueryRow(ctx, "SELECT state,retry_count,next_attempt_at FROM photo.tasks WHERE id=$1", task).Scan(&state, &retries, &next); err != nil {
				t.Fatal(err)
			}
			want := "ready"
			wantRetries := attempt
			if attempt == 4 {
				want = "failed"
				wantRetries = 3
			}
			if state != want || retries != wantRetries || next != nil {
				t.Fatal("budget state", attempt, state, retries, next)
			}
			// Duplicate scheduler passes must not consume another retry or revive failure.
			if _, err := recovery.Recover(ctx, 10); err != nil {
				t.Fatal(err)
			}
			var stable string
			var stableRetries int
			if err := admin.QueryRow(ctx, "SELECT state,retry_count FROM photo.tasks WHERE id=$1", task).Scan(&stable, &stableRetries); err != nil || stable != state || stableRetries != retries {
				t.Fatal("duplicate budget consumption", err)
			}
			if fingerprint() != before {
				t.Fatal("budget retry changed frozen inputs/profile")
			}
			if attempt < 4 {
				assertV27RetryObserverHealthy(t, ctx, admin, task, operation, false)
				raw, err := json.Marshal(old)
				if err != nil {
					t.Fatal(err)
				}
				previous = string(raw)
				_, cert, _ := wireCertificates(t)
				claimNativeWireTask(t, ctx, admin, task, certificateHash(cert.Leaf), operation, class, backend)
				if fingerprint() != before {
					t.Fatal("budget fresh claim changed frozen inputs/profile")
				}
			}
		}
		t.Log("V27_TARGET_WORKER_RETRY_EXHAUST_OK")
		t.Log("V27_WORKER_FAILURE_COMMITTED_OK")
		t.Log("V27_WORKER_WIRE_OK")
		return
	}
	runProbe(retry || oom, "")
	if n, err := (workflow.Recovery{DB: scheduler}).Recover(ctx, 10); err != nil || n < 1 {
		t.Fatal("scheduler failure classification", n, err)
	}
	if retry {
		var state, operation, class, backend string
		var next *time.Time
		var count int
		if err := admin.QueryRow(ctx, "SELECT state,next_attempt_at,retry_count,operation,resource_class,execution_profile->>'backend' FROM photo.tasks WHERE id=$1", task).Scan(&state, &next, &count, &operation, &class, &backend); err != nil || state != "retry_wait" || next == nil || count != 1 {
			t.Fatal("retry not scheduled", state, count, err)
		}
		assertV27RetryObserverHealthy(t, ctx, admin, task, operation, false)
		var initialDue *time.Time
		if err := admin.QueryRow(ctx, "SELECT max(coalesce(v27_initial_recovery_after,clock_timestamp())) FROM photo.runs WHERE tenant_id=(SELECT tenant_id FROM photo.tasks WHERE id=$1) AND v27_initial_recovery_enabled AND NOT v27_initial_recovery_done", task).Scan(&initialDue); err != nil || initialDue == nil {
			t.Fatal("initial recovery due time", err)
		}
		due := *next
		if initialDue.After(due) {
			due = *initialDue
		}
		delay := time.Until(due) + 20*time.Millisecond
		if delay > 7*time.Second {
			t.Fatal("unexpected retry delay", delay)
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
		assertV27RetryObserverHealthy(t, ctx, admin, task, operation, true)
		if n, err := (workflow.Recovery{DB: scheduler}).Recover(ctx, 10); err != nil || n < 1 {
			t.Fatal("retry activation", n, err)
		}
		if err := admin.QueryRow(ctx, "SELECT state FROM photo.tasks WHERE id=$1", task).Scan(&state); err != nil || state != "ready" {
			t.Fatal("retry not ready", state, err)
		}
		assertV27RetryObserverHealthy(t, ctx, admin, task, operation, true)
		_, cert, _ := wireCertificates(t)
		claimNativeWireTask(t, ctx, admin, task, certificateHash(cert.Leaf), operation, class, backend)
		if fingerprint() != before {
			t.Fatal("retry changed frozen target inputs/profile")
		}
		raw, err := json.Marshal(old)
		if err != nil {
			t.Fatal(err)
		}
		runProbe(false, string(raw))
		if n, err := (workflow.Recovery{DB: scheduler}).Recover(ctx, 10); err != nil || n < 1 {
			t.Fatal("second failure classification", n, err)
		}
		t.Log("V27_TARGET_WORKER_RETRY_ONCE_OK")
	}
	var state string
	if err := admin.QueryRow(ctx, "SELECT state FROM photo.tasks WHERE id=$1", task).Scan(&state); err != nil || state != "failed" {
		t.Fatal("worker failure not terminal", state, err)
	}
	if oom {
		if fingerprint() != before {
			t.Fatal("OOM changed frozen profile or inputs")
		}
		var retries, reductions int
		var next *time.Time
		if err := admin.QueryRow(ctx, "SELECT retry_count,oom_count,next_attempt_at FROM photo.tasks WHERE id=$1", task).Scan(&retries, &reductions, &next); err != nil || retries != 0 || reductions != 0 || next != nil {
			t.Fatal("unauthorized OOM retry", retries, reductions, next, err)
		}
		var snapshot, after string
		if err := admin.QueryRow(ctx, "SELECT to_jsonb(t)::text FROM photo.tasks t WHERE id=$1", task).Scan(&snapshot); err != nil {
			t.Fatal(err)
		}
		if _, err := (workflow.Recovery{DB: scheduler}).Recover(ctx, 10); err != nil {
			t.Fatal(err)
		}
		if err := admin.QueryRow(ctx, "SELECT to_jsonb(t)::text FROM photo.tasks t WHERE id=$1", task).Scan(&after); err != nil || after != snapshot {
			t.Fatal("duplicate recovery revived OOM task", err)
		}
		t.Log("V27_TARGET_WORKER_OOM_REFUSED_OK")
	}
	t.Log("V27_WORKER_FAILURE_COMMITTED_OK")
	t.Log("V27_WORKER_WIRE_OK")
}

func assertV27RetryObserverHealthy(t *testing.T, ctx context.Context, admin *pgxpool.Pool, task uuid.UUID, operation string, requireLease bool) {
	t.Helper()
	key := map[string]string{"mask.person": "v27_target_person_prepare", "face.detect": "v27_target_detector_prepare", "face.landmarks": "v27_target_face_prepare", "face.parse": "v27_target_face_prepare", "mask.face_skin": "v27_target_skin_prepare", "defects.propose": "v27_target_defect_prepare"}[operation]
	var tenant, project, actor, session, parent uuid.UUID
	if err := admin.QueryRow(ctx, "SELECT r.tenant_id,(r.config_snapshot->>'project_id')::uuid,r.created_by,(r.config_snapshot->$2->>'auth_session_id')::uuid,(r.config_snapshot->$2->>'parent_run_id')::uuid FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE t.id=$1", task, key).Scan(&tenant, &project, &actor, &session, &parent); err != nil {
		t.Fatal(err)
	}
	tx, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_api"); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		t.Fatal(err)
	}
	var observed *string
	if err := tx.QueryRow(ctx, "SELECT operation FROM photo.v27_native_initial_terminal_state($1,$2,$3,$4,$5)", tenant, project, actor, session, parent).Scan(&observed); err != nil || observed != nil {
		t.Fatal("pending retry retired recovery", observed, err)
	}
	if requireLease {
		if _, err := tx.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
			t.Fatal(err)
		}
		var detector, token uuid.UUID
		if err := tx.QueryRow(ctx, "SELECT detector_run_id,lease_token FROM photo.claim_v27_native_initial_pipeline($1,60)", project).Scan(&detector, &token); err != nil || detector != uuid.NewSHA1(parent, []byte("semantic.face.detect:reference")) {
			t.Fatal("pending recovery lease", err)
		}
		var recorded bool
		if err := tx.QueryRow(ctx, "SELECT photo.record_v27_native_initial_pipeline_handoff($1,$2,$3,'failed')", tenant, detector, token).Scan(&recorded); err != nil || recorded {
			t.Fatal("pending retry retired owned recovery", recorded, err)
		}
	}

}

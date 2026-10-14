package semanticproposal

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Actual saved child, reservation/claim, Python adapter, mTLS and immutable
// uploads. The nested probe explicitly uses a deterministic model test double.
func probeCreatePersonNewOutputs(t *testing.T, ctx context.Context, owner *pgxpool.Pool, tenant, parent uuid.UUID) {
	t.Helper()
	run, _, err := personPreparationIDs(parent)
	if err != nil {
		t.Fatal(err)
	}
	var task uuid.UUID
	if err := owner.QueryRow(ctx, "SELECT id FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2 AND operation='mask.person' AND state='ready'", tenant, run).Scan(&task); err != nil {
		t.Fatal("native person ready child", err)
	}
	probeNativeWireTaskOutputs(t, ctx, task)
	t.Log("V27_CREATE_PERSON_NEW_OUTPUTS_COMMITTED_OK")
}

func probeNativeWireTaskOutputs(t *testing.T, ctx context.Context, task uuid.UUID) {
	t.Helper()
	var fixture struct {
		Objects struct{ Endpoint, Bucket, AccessKey, SecretKey string }
	}
	if err := json.Unmarshal([]byte(os.Getenv("PHOTO_SEMANTIC_INTENT_PUBLISH_CASE")), &fixture); err != nil {
		t.Fatal(err)
	}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, "go", "test", "./internal/gateway", "-run", "^TestIntegrationV27IndexedDownloads$", "-count=1", "-timeout=90s", "-v")
	cmd.Dir = "../.."
	cmd.Env = append(os.Environ(), "PHOTO_TEST_V27_GATEWAY_TASK="+task.String(), "PHOTO_TEST_V27_REPO_ROOT="+root,
		"PHOTO_TEST_MINIO_URL="+fixture.Objects.Endpoint, "PHOTO_TEST_MINIO_BUCKET="+fixture.Objects.Bucket,
		"PHOTO_TEST_MINIO_ACCESS="+fixture.Objects.AccessKey, "PHOTO_TEST_MINIO_SECRET="+fixture.Objects.SecretKey)
	output, err := cmd.CombinedOutput()
	if os.Getenv("PHOTO_TEST_V27_NATIVE_TERMINAL_WORKER_FAILURE") == "1" && !strings.Contains(string(output), "V27_WORKER_FAILURE_COMMITTED_OK") {
		t.Fatal("worker failure branch not executed", string(output))
	}
	if os.Getenv("PHOTO_TEST_V27_TARGET_WORKER_RETRY_ONCE") == "1" && os.Getenv("PHOTO_TEST_V27_NATIVE_TERMINAL_WORKER_FAILURE") == "1" {
		if !strings.Contains(string(output), "V27_TARGET_WORKER_RETRY_ONCE_OK") {
			t.Fatal("retry branch not executed", string(output))
		}
		t.Log("V27_TARGET_WORKER_RETRY_ONCE_OK")
	}
	if os.Getenv("PHOTO_TEST_V27_TARGET_WORKER_RETRY_EXHAUST") == "1" && os.Getenv("PHOTO_TEST_V27_NATIVE_TERMINAL_WORKER_FAILURE") == "1" {
		if !strings.Contains(string(output), "V27_TARGET_WORKER_RETRY_EXHAUST_OK") {
			t.Fatal("budget branch not executed", string(output))
		}
		t.Log("V27_TARGET_WORKER_RETRY_EXHAUST_OK")
	}
	if os.Getenv("PHOTO_TEST_V27_FAILURE_CODE") == "RESOURCE_OOM" && os.Getenv("PHOTO_TEST_V27_NATIVE_TERMINAL_WORKER_FAILURE") == "1" {
		if !strings.Contains(string(output), "V27_TARGET_WORKER_OOM_REFUSED_OK") {
			t.Fatal("OOM refusal branch not executed", string(output))
		}
		t.Log("V27_TARGET_WORKER_OOM_REFUSED_OK")
	}
	if strings.Contains(string(output), "V27_TARGET_WORKER_RETRY_SUCCESS_OK") {
		t.Log("V27_TARGET_WORKER_RETRY_SUCCESS_OK")
	}
	if err != nil || !strings.Contains(string(output), "V27_WORKER_WIRE_OK") {
		t.Fatal("new native person wire output", err, string(output))
	}
}

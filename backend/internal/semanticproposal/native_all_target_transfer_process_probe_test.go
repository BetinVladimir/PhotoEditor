package semanticproposal

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type nativeAllTargetProcessRequest struct {
	Who             auth.Identity
	Tenant, Project uuid.UUID
	Key             string
	Body            json.RawMessage
	ColorSHA        string
	Expected        v2input.JobTicket
}

func probeNativeAllTargetTransferProcess(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, original detectorDraftPlan, color TransferColorProfile) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal("test executable unavailable")
	}
	body, err := json.Marshal(nativeAllTargetProcessRequest{who, original.request.TenantID, p.Reader.ProjectID, original.requestKey, original.request.Body, color.sha, v2input.JobTicket{RunID: original.request.RunID, WorkflowID: original.request.WorkflowID, State: "queued"}})
	if err != nil {
		t.Fatal("process request encoding")
	}
	cmd := exec.CommandContext(ctx, executable, "-test.run=^TestIntegrationNativeAllTargetTransferProcess$", "-test.v", "-test.timeout=180s")
	cmd.Env = append(os.Environ(), "PHOTO_TEST_V27_NATIVE_ALL_TARGET_CHILD=1")
	cmd.Stdin = bytes.NewReader(body)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("separate process recovery failed: %v\n%s", err, output)
	}
	if !bytes.Contains(output, []byte("V27_NATIVE_ALL_TARGET_CHILD_QUEUED_OK")) {
		t.Fatal("child recovery evidence missing")
	}
	t.Log("V27_NATIVE_ALL_TARGET_TRANSFER_SEPARATE_PROCESS_OK")
}

func TestIntegrationNativeAllTargetTransferProcess(t *testing.T) {
	if os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_CHILD") != "1" {
		t.Skip("separate-process fixture required")
	}
	var request nativeAllTargetProcessRequest
	decoder := json.NewDecoder(os.Stdin)
	decoder.DisallowUnknownFields()
	if decoder.Decode(&request) != nil {
		t.Fatal("invalid process request")
	}
	var fixture struct{ Objects objectstore.Config }
	if json.Unmarshal([]byte(os.Getenv("PHOTO_SEMANTIC_INTENT_PUBLISH_CASE")), &fixture) != nil {
		t.Fatal("invalid object fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("database configuration")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("API pool")
	}
	defer api.Close()
	schedulerConfig := cfg.Copy()
	schedulerConfig.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	scheduler, err := pgxpool.NewWithConfig(ctx, schedulerConfig)
	if err != nil {
		t.Fatal("scheduler pool")
	}
	defer scheduler.Close()
	objects, err := objectstore.New(fixture.Objects)
	if err != nil {
		t.Fatal("object configuration")
	}
	p := TransferDraftPreparer{Reader: SQLTransferSourceReader{DB: api, Objects: objects, Bucket: fixture.Objects.Bucket, ProjectID: request.Project}, Scheduler: scheduler}
	ticket, err := p.loadAndResumeNativeAllTargetTransfer(ctx, request.Who, request.Tenant, request.Key, request.Body, TransferColorProfile{sha: request.ColorSHA})
	if err != nil || ticket != request.Expected {
		t.Fatalf("child recovery: %v %v", ticket, err)
	}
	t.Log("V27_NATIVE_ALL_TARGET_CHILD_QUEUED_OK")
}

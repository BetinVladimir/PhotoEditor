package semanticproposal

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	runservice "photoeditor/backend/internal/run"
	"photoeditor/backend/internal/v2draft"
)

type nativeTargetTerminalNoIO struct{ calls int }

func (s *nativeTargetTerminalNoIO) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	s.calls++
	return nil, errors.New("terminal recovery read objects")
}
func (s *nativeTargetTerminalNoIO) PutImmutable(context.Context, string, io.Reader, int64, string, string) (objectstore.Object, error) {
	s.calls++
	return objectstore.Object{}, errors.New("terminal recovery published objects")
}
func (s *nativeTargetTerminalNoIO) RecoverImmutable(context.Context, string, int64, string, string) (objectstore.Object, error) {
	s.calls++
	return objectstore.Object{}, errors.New("terminal recovery repaired objects")
}

func TestIntegrationNativeTargetTerminalProcessOne(t *testing.T) {
	raw := os.Getenv("PHOTO_TEST_V27_TARGET_TERMINAL_CASE")
	if raw == "" {
		t.Skip("isolated target terminal fixture required")
	}
	var f struct {
		Tenant, Project, Actor, Session, Parent, Run uuid.UUID
		Mode                                         string
		Operation                                    string
	}
	if json.Unmarshal([]byte(raw), &f) != nil {
		t.Fatal("terminal fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	cfg = cfg.Copy()
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	scheduler, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer scheduler.Close()
	color, err := LoadTransferColorProfile("../../../resources/icc")
	if err != nil {
		t.Fatal(err)
	}
	objects := &nativeTargetTerminalNoIO{}
	publisher := &nativeRefusedRepairPublisher{}
	p := TransferDraftPreparer{Reader: SQLTransferSourceReader{DB: api, Objects: objects, Bucket: "test", ProjectID: f.Project}, Scheduler: scheduler,
		Documents: v2draft.Publisher{DB: api, Objects: objects, Bucket: "test"}, TransportDocuments: publisher,
		Configuration: TransferPreparation{Color: color, RAM: 6 << 30, VRAM: 1 << 30, Scratch: 1 << 30, Output: 64 << 20, MetadataBytes: 16 << 20}, ReservedBytes: 128 << 20}
	calls := 0
	factory := func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID) (v2draft.ArtifactCatalog, error) {
		calls++
		return nil, errors.New("terminal recovery resolved catalog")
	}
	job := SQLNativeInitialPipelineRecovery{Preparer: p, CatalogForReference: factory, Timeout: 30 * time.Second}
	who := auth.Identity{UserID: f.Actor, SessionID: f.Session}
	if f.Mode == "cancel" {
		if observed, err := p.Reader.readNativeInitialTerminalState(ctx, who, f.Tenant, f.Parent); err != nil || observed != "" {
			t.Fatal("healthy target terminal", observed, err)
		}
		for i := 0; i < 2; i++ {
			status, err := (runservice.Service{DB: api}).Cancel(ctx, who, f.Tenant, f.Run)
			if err != nil || status.State != "cancelling" {
				t.Fatal("authenticated target cancellation", status.State, err)
			}
		}
		if observed, err := p.Reader.readNativeInitialTerminalState(ctx, who, f.Tenant, f.Parent); err != nil || observed != f.Operation {
			t.Fatal("cancelling target observation", observed, err)
		}
		var n int
		if err := scheduler.QueryRow(ctx, "SELECT photo.cancel_idle_task()").Scan(&n); err != nil || n != 1 {
			t.Fatal("target scheduler cancellation", n, err)
		}
	}

	out, locked, err := job.Recover(ctx, who, f.Tenant, f.Parent)
	if err != nil || !locked || out.phase != nativePreparationFailed || out.operation != f.Operation {
		t.Fatal("terminal classification", out.phase, out.operation, locked, err)
	}
	if outcome, err := nativeInitialPipelineRecoveryOutcome(f.Parent, out, nil); err != nil || outcome != "failed" {
		t.Fatal("terminal authority", outcome, err)
	}
	if worked, err := job.ProcessOne(ctx); err != nil || !worked {
		t.Fatal("terminal ProcessOne", worked, err)
	}
	if worked, err := job.ProcessOne(ctx); err != nil || worked {
		t.Fatal("terminal reclaimed", worked, err)
	}
	if objects.calls != 0 || publisher.calls != 0 || calls != 0 {
		t.Fatal("terminal leaked IO", objects.calls, publisher.calls, calls)
	}
	t.Log("V27_TARGET_TERMINAL_PROCESS_ONE_OK")
}

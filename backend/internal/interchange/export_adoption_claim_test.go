package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestExportAdoptionClaimRequiresFrozenIdentityAndRequest(t *testing.T) {
	tenant, actor, snapshot, project := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	request := CreateRequest{ProjectID: project, Direction: "export", Format: "xmp", SnapshotID: &snapshot}
	sha, err := canonical.Hash(request)
	if err != nil {
		t.Fatal(err)
	}
	key := "export-adoption-claim"
	run := uuid.NewSHA1(tenant, []byte("interchange:create:"+actor.String()+":"+key))
	workflow := uuid.NewSHA1(run, []byte("workflow"))
	good := ExportAdoptionClaim{Tenant: tenant, Actor: actor, Run: run, Workflow: workflow,
		Task: uuid.NewSHA1(workflow, []byte("v2.interchange.export:0")), Result: uuid.New(),
		Token: uuid.New(), Key: key, RequestSHA: sha, Request: request}
	if err := good.validate(); err != nil {
		t.Fatal("valid scheduler discovery refused", err)
	}
	intent, err := exportIntentFromClaim(good)
	if err != nil || intent.Identity.Run != good.Run || intent.Identity.Task != good.Task ||
		intent.Identity.Snapshot != snapshot || intent.RequestSHA != sha ||
		intent.Identity.Params != uuid.NewSHA1(good.Task, []byte("interchange:params:1")) {
		t.Fatal("scheduler intent changed frozen identity", err)
	}
	*intent.Request.SnapshotID = uuid.New()
	if *good.Request.SnapshotID != snapshot {
		t.Fatal("scheduler intent aliased claimed request")
	}
	for name, mutate := range map[string]func(*ExportAdoptionClaim){
		"foreign tenant": func(c *ExportAdoptionClaim) { c.Tenant = uuid.New() },
		"foreign task":   func(c *ExportAdoptionClaim) { c.Task = uuid.New() },
		"missing lease":  func(c *ExportAdoptionClaim) { c.Token = uuid.Nil },
		"changed request": func(c *ExportAdoptionClaim) {
			c.Request.Format = "captureone_sidecar"
		},
		"changed hash": func(c *ExportAdoptionClaim) { c.RequestSHA = "wrong" },
		"invalid key":  func(c *ExportAdoptionClaim) { c.Key = "bad key" },
	} {
		t.Run(name, func(t *testing.T) {
			bad := good
			mutate(&bad)
			if err := bad.validate(); err != v2input.ErrInvalid {
				t.Fatal("substituted claim accepted", err)
			}
		})
	}
}

func TestExportAdoptionClaimStoreFailsClosedWithoutDatabase(t *testing.T) {
	store := SQLExportAdoptionClaimStore{}
	if _, err := store.NextExportAdoption(context.Background()); err != v2input.ErrInvalid {
		t.Fatal("missing database accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := store.NextExportAdoption(ctx); err != context.Canceled {
		t.Fatal("cancelled claim queried", err)
	}
	if current, err := store.CurrentExportAdoptionClaim(context.Background(), ExportAdoptionClaim{}); current || err != v2input.ErrInvalid {
		t.Fatal("missing current-claim database accepted", current, err)
	}
	if current, err := store.CurrentExportAdoptionClaim(ctx, ExportAdoptionClaim{}); current || err != context.Canceled {
		t.Fatal("cancelled current-claim queried", current, err)
	}
	if _, _, err := store.RootExportAdoption(context.Background(), ExportAdoptionClaim{}, "photos"); err != v2input.ErrInvalid {
		t.Fatal("root read without installation accepted", err)
	}
	if _, _, err := store.RootExportAdoption(ctx, ExportAdoptionClaim{}, "photos"); err != context.Canceled {
		t.Fatal("cancelled root read queried", err)
	}
	if _, _, err := store.FrozenExportAdoption(context.Background(), ExportAdoptionClaim{}); err != v2input.ErrInvalid {
		t.Fatal("frozen recovery without installation accepted", err)
	}
	if _, _, err := store.FrozenExportAdoption(ctx, ExportAdoptionClaim{}); err != context.Canceled {
		t.Fatal("cancelled frozen recovery queried", err)
	}
	catalog := SQLSchedulerExportOutputCatalog{Claims: store, Bucket: "photos"}
	if _, err := catalog.LookupExportOutput(context.Background(), v2input.Entry{}); err != v2input.ErrInvalid {
		t.Fatal("output catalog without installation accepted", err)
	}
	if _, err := catalog.LookupExportOutput(ctx, v2input.Entry{}); err != context.Canceled {
		t.Fatal("cancelled output lookup queried", err)
	}
}

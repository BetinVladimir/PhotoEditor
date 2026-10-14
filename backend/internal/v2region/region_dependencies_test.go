package v2region

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestRegionDependenciesUseEachPlanTenantProjectVersionRun(t *testing.T) {
	api, scheduler := new(pgxpool.Pool), new(pgxpool.Pool)
	objects := new(objectstore.Store)
	factory := SQLRegionDependencies{API: api, Scheduler: scheduler, Objects: objects, Bucket: "private"}
	plan, _, _, _ := regionNodeFixture(t)
	for i := 0; i < 2; i++ {
		doc, err := plan.decode()
		if err != nil {
			t.Fatal(err)
		}
		doc.ProjectID = uuid.New()
		doc.TenantID = uuid.New()
		plan.body, _ = canonical.Marshal(doc)
		plan.digest, _ = canonical.Hash(doc)
		run := uuid.New()
		deps, err := factory.ForRegionPlan(context.Background(), plan, run)
		if err != nil {
			t.Fatal(err)
		}
		resolver := deps.Resolver.(v2draft.ClosureResolver)
		catalog := resolver.Catalog.(v2draft.SQLArtifactCatalog)
		params := deps.Params.(v2draft.Publisher)
		closure := deps.Closure.(v2input.ScopedPublisher)
		docs := closure.Documents.(artifactjson.Publisher)
		repository := docs.Repository.(artifactjson.SQLRepository)
		if catalog.ProjectID != doc.ProjectID || catalog.AssetVersionID != doc.Request.VersionID || catalog.DB != api || params.DB != api ||
			closure.Tenant != doc.TenantID || closure.Run != run || repository.DB != scheduler {
			t.Fatal("request scope/roles lost")
		}
	}
	factory.Scheduler = api
	if _, err := factory.ForRegionPlan(context.Background(), plan, uuid.New()); !errors.Is(err, ErrRegionUnavailable) {
		t.Fatal("one role pool accepted", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := factory.ForRegionPlan(ctx, plan, uuid.New()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

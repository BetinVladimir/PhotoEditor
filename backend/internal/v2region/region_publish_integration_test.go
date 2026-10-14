package v2region

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2learning"
	"testing"
	"time"
)

func TestIntegrationRegionParamsPublication(t *testing.T) {
	raw := os.Getenv("PHOTO_REGION_PUBLISH_CASE")
	if raw == "" {
		t.Skip("isolated PostgreSQL and versioned object store required")
	}
	var f struct {
		Tenant, Project, Asset, Version, Actor, Session uuid.UUID
		Objects                                         objectstore.Config
		Request                                         *RegionPreviewRequest
	}
	if err := json.Unmarshal([]byte(raw), &f); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	objects, err := objectstore.New(f.Objects)
	if err != nil {
		t.Fatal("object store configuration")
	}
	var req RegionPreviewRequest
	var source, frozen RegionSourceMetadata
	var evidence CompiledRegionEvidence
	var plan FrozenRegionPlan
	checked := &regionPreparationReader{API: api, Objects: objects}
	if f.Request != nil {
		req = *f.Request
		prepared, prepareErr := (SQLRegionPreparation{API: api, Objects: checked, Bucket: f.Objects.Bucket}).Prepare(ctx, f.Tenant, f.Asset, f.Actor, f.Session, req)
		if prepareErr != nil {
			t.Fatal("live source/recipe preparation", prepareErr)
		}
		if checked.opens != 2 {
			t.Fatal("missing scoped preparation object reads", checked.opens)
		}
		plan, source, frozen, evidence = prepared.Plan, prepared.SourceMetadata, prepared.RecipeMetadata, prepared.Evidence

		shape, shapeErr := prepared.Plan.SourceShape()
		if shapeErr != nil || shape.Width != prepared.Evidence.Source.Canvas.Width || shape.Height != prepared.Evidence.Source.Canvas.Height || shape.HasAlpha != (prepared.Evidence.Source.Alpha != nil) {
			t.Fatal("pinned source shape", shape, shapeErr)
		}
		budget, budgetErr := shape.ResourceBudget()
		if budgetErr != nil {
			t.Fatal(budgetErr)
		}
		minimum := map[string]any{"pixel_count": budget.PixelCount, "ram_bytes": budget.RAMBytes, "scratch_bytes": budget.ScratchBytes}
		budgetProfile, _ := canonical.Marshal(map[string]any{"resource_estimate": minimum})
		if err := validateRegionSourceBudget(budgetProfile, shape); err != nil {
			t.Fatal("live source minimum budget", err)
		}
		minimum["scratch_bytes"] = budget.ScratchBytes - 1
		budgetProfile, _ = canonical.Marshal(map[string]any{"resource_estimate": minimum})
		if err := validateRegionSourceBudget(budgetProfile, shape); err == nil {
			t.Fatal("live source underbudget accepted")
		}
		if prepared.ProjectID != f.Project {
			t.Fatal("wrong project")
		}
		wrong := req
		wrong.VersionID = uuid.New()
		if _, err := (SQLRegionPreparation{API: api, Objects: checked, Bucket: f.Objects.Bucket}).Prepare(ctx, f.Tenant, f.Asset, f.Actor, f.Session, wrong); err == nil {
			t.Fatal("foreign source version accepted")
		}
	} else {
		fixtureReq, recipeRaw, sourceRef, sourceRaw := regionEvidenceFixture(t)
		req = fixtureReq
		req.VersionID = f.Version
		var recipe v2learning.FrozenRecipe
		if json.Unmarshal(recipeRaw, &recipe) != nil {
			t.Fatal("fixture recipe")
		}
		recipe.Source.AssetVersionID = f.Version
		recipeRaw, _ = canonical.Marshal(recipe)
		req.Recipe.SHA256, _ = canonical.Hash(recipe)
		evidence, err = CompileRegionEvidence(req, recipeRaw, sourceRef, sourceRaw)
		if err != nil {
			t.Fatal(err)
		}
		metadata := func(ref v2input.Ref, key string, body []byte) RegionSourceMetadata {
			return RegionSourceMetadata{ProjectID: f.Project, Ref: ref, Object: objectstore.Object{Key: key, VersionID: "fixture-pin", SHA256: ref.SHA256, Bytes: int64(len(body)), ContentType: "application/json"}}
		}
		source = metadata(sourceRef, "fixture-source", sourceRaw)
		frozen = metadata(v2input.Ref{ArtifactID: req.Recipe.ArtifactID, SHA256: req.Recipe.SHA256}, "fixture-recipe", recipeRaw)
		plan, err = freezeRegionPlan(f.Tenant, f.Asset, f.Project, f.Objects.Bucket, req, source, frozen, evidence)
		if err != nil {
			t.Fatal(err)
		}

	}
	publisher := v2draft.Publisher{DB: api, Objects: objects, Bucket: f.Objects.Bucket}
	who := auth.Identity{UserID: f.Actor, SessionID: f.Session}
	key := "region-disabled-admission"
	run, err := v2input.RunID(f.Tenant, who.UserID, key)
	if err != nil {
		t.Fatal(err)
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		t.Fatal(err)
	}
	task, err := v2input.TaskID(workflow, "region.preview", 0)
	if err != nil {
		t.Fatal(err)
	}
	first, err := plan.PublishParams(ctx, who, task, publisher)
	if err != nil {
		t.Fatal("publish", err)
	}
	replay, err := plan.PublishParams(ctx, who, task, publisher)
	if err != nil || replay != first {
		t.Fatal("replay", err)
	}
	policy, err := CompileRegionExecutionPolicy(plan, regionCostFixture(t, plan))
	if err != nil {
		t.Fatal(err)
	}
	profile := policy.Profile
	node, err := plan.CompileNode(task, first, plan.InputRequirements(), profile)
	if err != nil {
		t.Fatal(err)
	}
	transport := &regionTransportPublisher{}
	if _, err := node.Admit(ctx, api, transport, who, key, uuid.New(), regionSHA, 1024, regionSHA); !errors.Is(err, ErrRegionInvalid) {
		t.Fatal("disabled region admission must fail at live SQL gate", err)
	}
	if transport.calls != 0 {
		t.Fatal("disabled admission published input closure")
	}
	req.MaxEdge = 512
	evidence.Geometry, err = CompileRegionGeometry(evidence.Source.Canvas.Width, evidence.Source.Canvas.Height, req.Rect, req.MaxEdge)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := freezeRegionPlan(f.Tenant, f.Asset, f.Project, f.Objects.Bucket, req, source, frozen, evidence)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := changed.PublishParams(ctx, who, task, publisher); err == nil || domain.AsError(err).Code != "REVISION_CONFLICT" {
		t.Fatal("same task must reject changed Params with revision conflict", err)
	}
}

// Actual object reads must observe a released API transaction/connection.
type regionPreparationReader struct {
	API     *pgxpool.Pool
	Objects *objectstore.Store
	opens   int
}

func (r *regionPreparationReader) Open(ctx context.Context, obj objectstore.Object) (io.ReadCloser, error) {
	if r.API.Stat().AcquiredConns() != 0 {
		return nil, errors.New("object IO retained an API transaction")
	}
	r.opens++
	return r.Objects.Open(ctx, obj)
}

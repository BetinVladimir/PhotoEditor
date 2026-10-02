package v2render

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"testing"
	"time"
)

func TestIntegrationCompositionParamsPublication(t *testing.T) {
	raw := os.Getenv("PHOTO_COMPOSITION_PUBLISH_CASE")
	if raw == "" {
		t.Skip("isolated database and versioned object store required")
	}
	var fixture struct {
		Tenant, Project, Snapshot, Actor, Session uuid.UUID
		Params, Profile                           string
		Width, Height                             int64
		Objects                                   objectstore.Config
		ExistingRun                               uuid.UUID
		RevokedTransport                          bool
		ManifestBody                              string
	}
	if err := json.Unmarshal([]byte(raw), &fixture); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	poolFor := func(role string) *pgxpool.Pool {
		config, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
		if err != nil {
			t.Fatal(err)
		}
		config.ConnConfig.RuntimeParams["role"] = role
		pool, err := pgxpool.NewWithConfig(ctx, config)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		return pool
	}
	api := poolFor("photo_api")
	scheduler := poolFor("photo_scheduler")
	objects, err := objectstore.New(fixture.Objects)
	if err != nil {
		t.Fatal("object store configuration invalid")
	}
	if fixture.RevokedTransport {
		workflow, _ := v2input.WorkflowID(fixture.ExistingRun)
		task, _ := v2input.TaskID(workflow, "layer.compose", 0)
		publisher := artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: scheduler, Bucket: fixture.Objects.Bucket}, Objects: objects}
		_, err := publisher.Publish(ctx, artifactjson.Scope{Tenant: fixture.Tenant, Run: fixture.ExistingRun, Owner: task, Purpose: "v2_input_manifest"}, []byte(fixture.ManifestBody))
		if !errors.Is(err, artifactjson.ErrStale) {
			t.Fatal("revoked session replayed transport", err)
		}
		return
	}
	var params LayerComposeParams
	if err := json.Unmarshal([]byte(fixture.Params), &params); err != nil {
		t.Fatal(err)
	}
	who := auth.Identity{UserID: fixture.Actor, SessionID: fixture.Session}
	run := uuid.New()
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		t.Fatal(err)
	}
	task, err := v2input.TaskID(workflow, "layer.compose", 0)
	if err != nil {
		t.Fatal(err)
	}
	request := CompositionPreparation{Tenant: fixture.Tenant, Task: task, Actor: who, Base: params.Base, SourceResult: params.SourceResult, ProposalResult: params.ProposalResult, ProtectedMask: params.ProtectedMask, Width: fixture.Width, Height: fixture.Height, Layer: Layer{ID: uuid.New(), Enabled: true, Opacity: params.Opacity, Blend: params.Blend, Operation: "fixture.adjustment", OperationVersion: "1", Params: params.ProposalResult, Mask: params.Mask}}
	publisher := v2draft.Publisher{DB: api, Objects: objects, Bucket: fixture.Objects.Bucket}
	first, err := PublishCompositionParams(ctx, request, publisher)
	if err != nil {
		t.Fatal("publication", err)
	}
	second, err := PublishCompositionParams(ctx, request, publisher)
	if err != nil || second.ParamsRef != first.ParamsRef {
		t.Fatal("publication replay", err)
	}
	preflight := SQLOriginalCompositionPreflight{DB: scheduler}
	if err := preflight.CheckParams(ctx, fixture.Tenant, fixture.Project, fixture.Snapshot, who, first.ParamsRef, json.RawMessage(fixture.Params), fixture.Width, fixture.Height, json.RawMessage(fixture.Profile)); err != nil {
		t.Fatal("published params scope", err)
	}
	if err := preflight.CheckTaskParams(ctx, fixture.Tenant, fixture.Project, fixture.Snapshot, who, task, first.ParamsRef, json.RawMessage(fixture.Params), fixture.Width, fixture.Height, json.RawMessage(fixture.Profile)); err != nil {
		t.Fatal("task-bound params scope", err)
	}
	if err := preflight.CheckTaskParams(ctx, fixture.Tenant, fixture.Project, fixture.Snapshot, who, uuid.New(), first.ParamsRef, json.RawMessage(fixture.Params), fixture.Width, fixture.Height, json.RawMessage(fixture.Profile)); !errors.Is(err, v2input.ErrExpired) {
		t.Fatal("foreign task reused published params", err)
	}
	compiled, err := CompileComposition(first.Layer, first.Base, first.SourceResult, first.ProposalResult, first.ProtectedMask, first.Width, first.Height)
	if err != nil {
		t.Fatal(err)
	}
	roots, err := CompositionInputRoots(compiled, first.ParamsRef)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := CompileCompositionDraftConfig(ctx, fixture.Project, fixture.Snapshot, fixture.Session, run, []CompositionDraftRequest{{Ordinal: 0, Preparation: first, Resolved: roots}})
	if err != nil {
		t.Fatal("compile composition plan", err)
	}
	if err := preflight.CheckDraft(ctx, fixture.Tenant, run, who, plan); err != nil {
		t.Fatal("published original composition plan scope", err)
	}
	if err := preflight.CheckDraft(ctx, fixture.Tenant, uuid.New(), who, plan); !errors.Is(err, v2input.ErrExpired) {
		t.Fatal("foreign run accepted frozen plan", err)
	}
	wrongPlan := plan
	wrongPlan.OutputBytes++
	if err := preflight.CheckDraft(ctx, fixture.Tenant, run, who, wrongPlan); !errors.Is(err, v2input.ErrExpired) {
		t.Fatal("altered quota accepted", err)
	}
	var accepted bool
	if err := scheduler.QueryRow(ctx, "SELECT photo.v25_layer_plan_shape($1,$2::jsonb,$3)", workflow, plan.Body, plan.OutputBytes).Scan(&accepted); err != nil || !accepted {
		t.Fatal("Go plan rejected by SQL shape guard", err)
	}
	drafts := SQLOriginalCompositionDraftStore{DB: &preflight}
	parallel := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { parallel <- drafts.Begin(ctx, fixture.Tenant, run, who, first.ParamsRef.SHA256, plan) }()
	}
	for i := 0; i < 2; i++ {
		if err := <-parallel; err != nil {
			t.Fatal("concurrent draft replay", err)
		}
	}
	for i := 0; i < 2; i++ {
		if err := drafts.Begin(ctx, fixture.Tenant, run, who, first.ParamsRef.SHA256, plan); err != nil {
			t.Fatal("begin/replay composition draft", err)
		}
	}
	if err := drafts.Begin(ctx, fixture.Tenant, run, who, params.ProposalResult.SHA256, plan); !errors.Is(err, v2input.ErrExpired) {
		t.Fatal("conflicting policy accepted", err)
	}
	stage := SQLOriginalCompositionTaskStage{DB: scheduler}
	staged := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { staged <- stage.Stage(ctx, fixture.Tenant, run, workflow, 0, plan.Tasks[0]) }()
	}
	for i := 0; i < 2; i++ {
		if err := <-staged; err != nil {
			t.Fatal("concurrent task staging", err)
		}
	}
	if err := stage.Stage(ctx, fixture.Tenant, run, workflow, 0, plan.Tasks[0]); err != nil {
		t.Fatal("stage replay", err)
	}
	wrongTask := plan.Tasks[0]
	wrongTask.InputDigest = params.ProposalResult.SHA256
	if err := stage.Stage(ctx, fixture.Tenant, run, workflow, 0, wrongTask); !errors.Is(err, v2input.ErrExpired) {
		t.Fatal("altered input digest staged", err)
	}
	transport := CompositionTransportPublisher{Documents: artifactjson.Publisher{Repository: artifactjson.SQLRepository{DB: scheduler, Bucket: fixture.Objects.Bucket}, Objects: objects}}
	graph, err := transport.Publish(ctx, fixture.Tenant, run, 0, plan.Inputs[0])
	if err != nil {
		t.Fatal("composition transport publication", err)
	}
	replayed, err := transport.Publish(ctx, fixture.Tenant, run, 0, plan.Inputs[0])
	if err != nil || graph.Built.Manifest != replayed.Built.Manifest || len(graph.Built.Chunks) != 1 || graph.Built.Chunks[0] != replayed.Built.Chunks[0] {
		t.Fatal("composition transport replay", err)
	}
	catalog := v2draft.SQLArtifactCatalog{DB: api, Bucket: fixture.Objects.Bucket, ProjectID: fixture.Project, AssetVersionID: params.Base.AssetVersionID}
	object, err := catalog.Lookup(ctx, who, fixture.Tenant, v2draft.LayerRef{Ref: first.ParamsRef, Contract: "Params"})
	if err != nil {
		t.Fatal("published params lookup", err)
	}
	reader, err := objects.Open(ctx, object)
	if err != nil {
		t.Fatal("pinned params open", err)
	}
	body, readErr := io.ReadAll(io.LimitReader(reader, object.Bytes+1))
	closeErr := reader.Close()
	if readErr != nil || closeErr != nil || string(body) != fixture.Params {
		t.Fatal("pinned params content differs", readErr, closeErr)
	}
}

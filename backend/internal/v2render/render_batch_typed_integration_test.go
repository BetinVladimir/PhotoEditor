package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestIntegrationRenderBatchTypedPlan(t *testing.T) {
	path := os.Getenv("PHOTO_RENDER_TYPED_FIXTURE")
	if path == "" {
		t.Skip("typed render database fixture required")
	}
	var f struct {
		Tenant, Project, Snapshot, Batch, Actor, Session, Root uuid.UUID
		Binding                                                v2input.SessionVersionBinding
		Source                                                 json.RawMessage
		Style                                                  v2input.Ref
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	source, err := canonical.Bytes(f.Source)
	if err != nil {
		t.Fatal(err)
	}
	authored, err := canonical.Marshal(map[string]any{"schema_version": 3, "source": f.Binding, "base_style": f.Style, "session_corrections": nil, "learning_version_id": nil, "overrides": []any{}, "layer_stack": nil, "renderer_version": "v2.2", "parameter_delta": v2draft.CorrectionVector{}})
	if err != nil {
		t.Fatal(err)
	}
	recipe, err := v2draft.BuildFrozenRenderRecipe(authored, v2draft.ResolvedRecipeInputs{Binding: f.Binding, BaseStyle: f.Style, Style: v2draft.StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, ICCLockSHA: strings.Repeat("a", 64), ModelLockSHA: strings.Repeat("b", 64)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	recipeSHA, err := canonical.Hash(json.RawMessage(recipe))
	if err != nil {
		t.Fatal(err)
	}
	root := v2input.Ref{ArtifactID: f.Root, SHA256: recipeSHA}
	style, err := os.ReadFile("../../../docs/implementation/contracts/examples/style-fixed.example.json")
	if err != nil {
		t.Fatal(err)
	}
	style, err = canonical.Bytes(style)
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(source)
	if err != nil {
		t.Fatal(err)
	}
	catalog := fullRenderClosureStore{objects: map[uuid.UUID]objectstore.Object{}, bodies: map[string][]byte{}}
	for _, item := range []struct {
		ref  v2input.Ref
		body []byte
	}{{f.Binding.Source, source}, {f.Style, style}, {root, recipe}} {
		key := item.ref.ArtifactID.String()
		catalog.objects[item.ref.ArtifactID] = objectstore.Object{Key: key, SHA256: item.ref.SHA256, Bytes: int64(len(item.body)), VersionID: "v1", ContentType: "application/json"}
		catalog.bodies[key] = item.body
	}
	catalog.objects[image.Pixels.ID] = objectstore.Object{Key: "pixels", SHA256: image.Pixels.SHA256, Bytes: 64, VersionID: "v1", ContentType: "application/x-npy"}
	who := auth.Identity{UserID: f.Actor, SessionID: f.Session}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	entries, err := ResolveRenderInputs(ctx, who, f.Tenant, f.Binding, root, recipe, source, v2draft.ClosureResolver{Catalog: catalog, Objects: catalog})
	if err != nil {
		t.Fatal(err)
	}
	request := RenderDraftRequest{Tenant: f.Tenant, Actor: who, Binding: f.Binding, RecipeRef: root, RecipeRaw: recipe, SourceRaw: source, Backend: "cuda", RuntimeSHA: strings.Repeat("a", 64), TileSize: 512, AlphaMode: "require_float32"}
	parts, err := CompileRenderBatchPlans(f.Batch, f.Project, f.Snapshot, []RenderDraftRequest{request}, [][]v2input.Entry{entries})
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("PHOTO_RENDER_TYPED_STAGE") == "" {
		if err = os.WriteFile(path+".recipe", recipe, 0600); err != nil {
			t.Fatal(err)
		}
		return
	}
	cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := SQLRenderDraftStore{DB: pool}
	admin, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	var beforeReserved int64
	if err := admin.QueryRow(ctx, "SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1", f.Tenant).Scan(&beforeReserved); err != nil {
		t.Fatal(err)
	}
	// Parent admission succeeds on live selection metadata; expired recipe rejects its child.
	if _, err := admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", f.Root); err != nil {
		t.Fatal(err)
	}
	manifest, err := BuildRenderBatchManifest(f.Batch, f.Project, f.Snapshot, who, strings.Repeat("a", 64), parts)
	if err != nil {
		t.Fatal(err)
	}
	probe, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var admitted bool
	err = probe.QueryRow(ctx, "SELECT photo.begin_v26_render_batch($1,$2,$3,$4,$5,$6,$7)", f.Tenant, f.Project, f.Snapshot, who.UserID, who.SessionID, f.Batch, string(manifest)).Scan(&admitted)
	rollbackErr := probe.Rollback(ctx)
	if err != nil || !admitted || rollbackErr != nil {
		t.Fatal("parent admission before rejected part", admitted, err, rollbackErr)
	}
	if err := store.BeginBatch(ctx, f.Tenant, f.Batch, f.Project, f.Snapshot, who, strings.Repeat("a", 64), parts); !errors.Is(err, v2input.ErrExpired) {
		t.Fatal("expired part admission", err)
	}
	var leftover int
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE id=ANY($1::uuid[])", []uuid.UUID{f.Batch, parts[0].RunID}).Scan(&leftover); err != nil || leftover != 0 {
		t.Fatal("batch rollback left runs", leftover, err)
	}
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM photo.quota_reservations WHERE tenant_id=$1 AND holder_id=ANY($2::uuid[])", f.Tenant, []uuid.UUID{f.Batch, parts[0].RunID}).Scan(&leftover); err != nil || leftover != 0 {
		t.Fatal("batch rollback left reservations", leftover, err)
	}
	var afterReserved int64
	if err := admin.QueryRow(ctx, "SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1", f.Tenant).Scan(&afterReserved); err != nil || afterReserved != beforeReserved {
		t.Fatal("batch rollback changed quota", beforeReserved, afterReserved, err)
	}
	if _, err := admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", f.Root); err != nil {
		t.Fatal(err)
	}
	if err := store.BeginBatch(ctx, f.Tenant, f.Batch, f.Project, f.Snapshot, who, strings.Repeat("a", 64), parts); err != nil {
		t.Fatal("begin full typed batch", err)
	}
	if err := store.BeginBatch(ctx, f.Tenant, f.Batch, f.Project, f.Snapshot, who, strings.Repeat("a", 64), parts); err != nil {
		t.Fatal("replay", err)
	}
	saved, err := store.LoadBatchPart(ctx, f.Tenant, f.Batch, who, 0)
	if err != nil {
		t.Fatal("recover typed part", err)
	}
	if saved.ConfigSHA != parts[0].Plan.ConfigSHA || saved.ReservedBytes != parts[0].Plan.OutputBytes {
		t.Fatal("recovery changed evidence")
	}
	if _, err := DecodeRecoveredRenderPlan(saved); err != nil {
		t.Fatal(err)
	}
	if err := store.CancelBatch(ctx, f.Tenant, f.Batch, who); err != nil {
		t.Fatal("cancel typed batch", err)
	}
	if _, err := store.LoadBatchPart(ctx, f.Tenant, f.Batch, who, 0); !errors.Is(err, v2input.ErrExpired) {
		t.Fatal("cancelled part remains recoverable", err)
	}
	parent, err := store.LoadBatch(ctx, f.Tenant, f.Batch, who)
	if err != nil || !parent.CancelRequested {
		t.Fatal("cancelled manifest evidence", err)
	}

	// Two distinct asset versions sharing controlled source bytes exercise a late-part failure.
	asset2, version2, root2, document2, snapshot2, multi := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if _, err := admin.Exec(ctx, `INSERT INTO photo.assets(id,tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) SELECT $1,a.tenant_id,a.project_id,a.original_artifact_id,'{}','verified','{}' FROM photo.assets a JOIN photo.asset_versions v ON v.tenant_id=a.tenant_id AND v.asset_id=a.id WHERE v.id=$2`, asset2, f.Binding.AssetVersionID); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO photo.asset_versions(id,tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) SELECT $1,tenant_id,$2,master_artifact_id,content_hash,'{}','accepted' FROM photo.asset_versions WHERE id=$3`, version2, asset2, f.Binding.AssetVersionID); err != nil {
		t.Fatal(err)
	}
	binding2 := f.Binding
	binding2.AssetVersionID = version2
	var authored2 map[string]any
	if err := json.Unmarshal(authored, &authored2); err != nil {
		t.Fatal(err)
	}
	authored2["source"] = binding2
	raw2, err := canonical.Marshal(authored2)
	if err != nil {
		t.Fatal(err)
	}
	recipe2, err := v2draft.BuildFrozenRenderRecipe(raw2, v2draft.ResolvedRecipeInputs{Binding: binding2, BaseStyle: f.Style, Style: v2draft.StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, ICCLockSHA: strings.Repeat("a", 64), ModelLockSHA: strings.Repeat("b", 64)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	sha2, err := canonical.Hash(json.RawMessage(recipe2))
	if err != nil {
		t.Fatal(err)
	}
	rootBucket, rootVersion := "test", "v1"
	if raw := os.Getenv("PHOTO_RENDER_OBJECT_CONFIG"); raw != "" {
		var config objectstore.Config
		if json.Unmarshal([]byte(raw), &config) != nil {
			t.Fatal("invalid object fixture configuration")
		}
		client, err := objectstore.New(config)
		if err != nil {
			t.Fatal("object fixture unavailable")
		}
		obj, err := client.PutImmutable(ctx, root2.String(), bytes.NewReader(recipe2), int64(len(recipe2)), sha2, "application/json")
		if err != nil {
			t.Fatal("upload second root", err)
		}
		rootBucket, rootVersion = config.Bucket, obj.VersionID
	}
	if _, err := admin.Exec(ctx, `INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES($1,$2,$3,$4,$5,$6,$7,'application/json','document',now())`, root2, f.Tenant, rootBucket, root2.String(), rootVersion, sha2, len(recipe2)); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,project_id,asset_version_id,artifact_id,sha256,bytes,bucket,state,object_version,completed_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,'ready',$9,now())`, f.Tenant, document2, f.Project, version2, root2, sha2, len(recipe2), rootBucket, rootVersion); err != nil {
		t.Fatal(err)
	}
	registration, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registration.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", f.Tenant.String()); err != nil {
		t.Fatal(err)
	}
	if _, err := registration.Exec(ctx, "SET LOCAL ROLE photo_api"); err != nil {
		t.Fatal(err)
	}
	var registered bool
	if err := registration.QueryRow(ctx, "SELECT photo.register_v26_render_recipe($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", f.Tenant, version2, document2, who.UserID, who.SessionID, root2, sha2, binding2.Source.ArtifactID, binding2.Source.SHA256, string(recipe2)).Scan(&registered); err != nil || !registered {
		registration.Rollback(ctx)
		t.Fatal("second typed recipe registration", registered, err)
	}
	if err := registration.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `INSERT INTO photo.snapshots(id,tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at) VALUES($1,$2,'v2','user',$3,$4,2,clock_timestamp()+interval '1 hour')`, snapshot2, f.Tenant, who.UserID, strings.Repeat("d", 64)); err != nil {
		t.Fatal(err)
	}
	for ordinal, version := range []uuid.UUID{f.Binding.AssetVersionID, version2} {
		if _, err := admin.Exec(ctx, `INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload) VALUES($1,$2,$3,$4,$5,$6,'{}')`, f.Tenant, snapshot2, ordinal, version, binding2.Source.ArtifactID, binding2.Source.SHA256); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := admin.Exec(ctx, "UPDATE photo.snapshots SET expires_at=NULL WHERE id=$1", snapshot2); err != nil {
		t.Fatal(err)
	}
	ref2 := v2input.Ref{ArtifactID: root2, SHA256: sha2}
	catalog.objects[root2] = objectstore.Object{Key: root2.String(), SHA256: sha2, Bytes: int64(len(recipe2)), VersionID: "v1", ContentType: "application/json"}
	catalog.bodies[root2.String()] = recipe2
	entries2, err := ResolveRenderInputs(ctx, who, f.Tenant, binding2, ref2, recipe2, source, v2draft.ClosureResolver{Catalog: catalog, Objects: catalog})
	if err != nil {
		t.Fatal(err)
	}
	second := request
	second.Binding = binding2
	second.RecipeRef = ref2
	second.RecipeRaw = recipe2
	multiParts := make([]CompiledRenderBatchPart, 2)
	for ordinal, item := range []RenderDraftRequest{request, second} {
		id := uuid.NewSHA1(multi, []byte("render.batch:"+strconv.Itoa(ordinal)))
		inventory := entries
		if ordinal == 1 {
			inventory = entries2
		}
		plan, err := CompileRenderRunPlan(id, f.Project, snapshot2, []RenderDraftRequest{item}, [][]v2input.Entry{inventory})
		if err != nil {
			t.Fatal(err)
		}
		multiParts[ordinal] = CompiledRenderBatchPart{RunID: id, FirstOrdinal: ordinal, Plan: plan}
	}
	if _, err := admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", root2); err != nil {
		t.Fatal(err)
	}
	multiManifest, err := BuildRenderBatchManifest(multi, f.Project, snapshot2, who, strings.Repeat("a", 64), multiParts)
	if err != nil {
		t.Fatal(err)
	}
	probe, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := probe.QueryRow(ctx, "SELECT photo.begin_v26_render_batch($1,$2,$3,$4,$5,$6,$7)", f.Tenant, f.Project, snapshot2, who.UserID, who.SessionID, multi, string(multiManifest)).Scan(&admitted); err != nil || !admitted {
		t.Fatal("multi parent admission", admitted, err)
	}
	if err := probe.QueryRow(ctx, "SELECT photo.begin_v26_render_batch_part($1,$2,$3,$4,0,$5)", f.Tenant, multi, who.UserID, who.SessionID, string(multiParts[0].Plan.Config)).Scan(&admitted); err != nil || !admitted {
		t.Fatal("first part admission", admitted, err)
	}
	var stagedTasks int
	if err := probe.QueryRow(ctx, "SELECT task_count FROM photo.v26_render_batch_parts($1,$2,$3,$4) WHERE part_ordinal=0", f.Tenant, multi, who.UserID, who.SessionID).Scan(&stagedTasks); err != nil || stagedTasks != 1 {
		t.Fatal("first part did not stage task", stagedTasks, err)
	}
	if err := probe.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err := admin.QueryRow(ctx, "SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1", f.Tenant).Scan(&beforeReserved); err != nil {
		t.Fatal(err)
	}
	if err := store.BeginBatch(ctx, f.Tenant, multi, f.Project, snapshot2, who, strings.Repeat("a", 64), multiParts); !errors.Is(err, v2input.ErrExpired) {
		t.Fatal("late part rejection", err)
	}
	ids := []uuid.UUID{multi, multiParts[0].RunID, multiParts[1].RunID}
	for _, query := range []string{"SELECT count(*) FROM photo.runs WHERE id=ANY($1::uuid[])", "SELECT count(*) FROM photo.tasks WHERE run_id=ANY($1::uuid[])", "SELECT count(*) FROM photo.quota_reservations WHERE holder_id=ANY($1::uuid[])"} {
		if err := admin.QueryRow(ctx, query, ids).Scan(&leftover); err != nil || leftover != 0 {
			t.Fatal("late-part rollback", leftover, err)
		}
	}
	if err := admin.QueryRow(ctx, "SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1", f.Tenant).Scan(&afterReserved); err != nil || afterReserved != beforeReserved {
		t.Fatal("late-part quota rollback", beforeReserved, afterReserved, err)
	}
	if _, err := admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", root2); err != nil {
		t.Fatal(err)
	}
	dropCfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	dropCfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	// The isolated local server permits plaintext; protocol inspection never logs bytes.
	dropCfg.ConnConfig.TLSConfig = nil
	dropCfg.ConnConfig.Fallbacks = nil
	var dropped atomic.Bool
	dropCfg.ConnConfig.DialFunc = func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return &commitDropConn{Conn: conn, dropped: &dropped}, nil
	}
	uncertainPool, err := pgxpool.NewWithConfig(ctx, dropCfg)
	if err != nil {
		t.Fatal(err)
	}
	uncertainStore := SQLRenderDraftStore{DB: uncertainPool}
	uncertainErr := uncertainStore.BeginBatch(ctx, f.Tenant, multi, f.Project, snapshot2, who, strings.Repeat("a", 64), multiParts)
	uncertainPool.Close()
	if uncertainErr == nil || !dropped.Load() {
		t.Fatal("COMMIT response loss not injected", uncertainErr, dropped.Load())
	}
	// The server committed, but the creating store received no successful reply.
	for _, id := range ids {
		var count int
		if err := admin.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE id=$1", id).Scan(&count); err != nil || count != 1 {
			t.Fatal("unknown commit persistence", count, err)
		}
	}
	if err := store.BeginBatch(ctx, f.Tenant, multi, f.Project, snapshot2, who, strings.Repeat("a", 64), multiParts); err != nil {
		t.Fatal("multi retry", err)
	}
	if err := store.BeginBatch(ctx, f.Tenant, multi, f.Project, snapshot2, who, strings.Repeat("a", 64), multiParts); err != nil {
		t.Fatal("multi replay", err)
	}
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM photo.tasks WHERE run_id=ANY($1::uuid[])", ids).Scan(&leftover); err != nil || leftover != 2 {
		t.Fatal("multi replay task cardinality", leftover, err)
	}
	for ordinal := range multiParts {
		if _, err := store.LoadBatchPart(ctx, f.Tenant, multi, who, ordinal); err != nil {
			t.Fatal("multi recovery", ordinal, err)
		}
	}

	manifestSHA, err := canonical.Hash(json.RawMessage(multiManifest))
	if err != nil {
		t.Fatal(err)
	}
	checkpoint := renderBatchRestartEvidence{Tenant: f.Tenant, Batch: multi, Project: f.Project, Snapshot: snapshot2, Actor: who, ManifestSHA: manifestSHA}
	for _, part := range multiParts {
		checkpoint.Parts = append(checkpoint.Parts, renderBatchRestartPart{RunID: part.RunID, ConfigSHA: part.Plan.ConfigSHA, OutputBytes: part.Plan.OutputBytes})
	}
	checkpointRaw, err := json.Marshal(checkpoint)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".recovery", checkpointRaw, 0600); err != nil {
		t.Fatal(err)
	}
	if os.Getenv("PHOTO_RENDER_TYPED_CRASH") == "1" {
		process, err := os.FindProcess(os.Getpid())
		if err != nil {
			t.Fatal(err)
		}
		if err := process.Kill(); err != nil {
			t.Fatal("self-kill injection failed", err)
		}
		t.Fatal("self-kill injection unexpectedly returned")
	}

}

type renderBatchRestartPart struct {
	RunID       uuid.UUID
	ConfigSHA   string
	OutputBytes int64
}
type renderBatchRestartEvidence struct {
	Tenant, Batch, Project, Snapshot uuid.UUID
	Actor                            auth.Identity
	ManifestSHA                      string
	Parts                            []renderBatchRestartPart
}

func TestIntegrationRenderBatchTypedRestart(t *testing.T) {
	path := os.Getenv("PHOTO_RENDER_TYPED_FIXTURE")
	if path == "" {
		t.Skip("persisted typed batch fixture required")
	}
	raw, err := os.ReadFile(path + ".recovery")
	if err != nil {
		t.Fatal(err)
	}
	var evidence renderBatchRestartEvidence
	if err := json.Unmarshal(raw, &evidence); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	store := SQLRenderDraftStore{DB: pool}
	parent, err := store.LoadBatch(ctx, evidence.Tenant, evidence.Batch, evidence.Actor)
	if err != nil || parent.ManifestSHA != evidence.ManifestSHA || parent.CancelRequested || len(parent.Manifest.Parts) != len(evidence.Parts) {
		t.Fatal("restart parent", err)
	}
	// External object IO remains controlled; every recipe/source is recovered from SQL.
	catalog := fullRenderClosureStore{objects: map[uuid.UUID]objectstore.Object{}, bodies: map[string][]byte{}}
	style, err := os.ReadFile("../../../docs/implementation/contracts/examples/style-fixed.example.json")
	if err != nil {
		t.Fatal(err)
	}
	style, err = canonical.Bytes(style)
	if err != nil {
		t.Fatal(err)
	}
	var roots []uuid.UUID
	for ordinal, want := range evidence.Parts {
		saved, err := store.LoadBatchPart(ctx, evidence.Tenant, evidence.Batch, evidence.Actor, ordinal)
		if err != nil || saved.RunID != want.RunID || saved.ProjectID != evidence.Project || saved.SnapshotID != evidence.Snapshot || saved.ConfigSHA != want.ConfigSHA || saved.ReservedBytes != want.OutputBytes {
			t.Fatal("restart child", ordinal, err)
		}
		nodes, err := DecodeRecoveredRenderPlan(saved)
		if err != nil {
			t.Fatal("restart typed decode", ordinal, err)
		}
		for _, recovered := range nodes {
			node := recovered.Node
			scalars := recovered.Evidence.Recipe.Scalars
			for _, item := range []struct {
				ref  v2input.Ref
				body []byte
			}{
				{node.Params, []byte(node.ParamsJCS)},
				{scalars.Source.Source, []byte(node.SourceJCS)},
				{scalars.BaseStyle, style},
			} {
				key := item.ref.ArtifactID.String()
				catalog.objects[item.ref.ArtifactID] = objectstore.Object{Key: key, SHA256: item.ref.SHA256, Bytes: int64(len(item.body)), VersionID: "v1", ContentType: "application/json"}
				catalog.bodies[key] = item.body
			}
			image, err := review.ParseWorkingImage([]byte(node.SourceJCS))
			if err != nil {
				t.Fatal(err)
			}
			catalog.objects[image.Pixels.ID] = objectstore.Object{Key: "pixels", SHA256: image.Pixels.SHA256, Bytes: 64, VersionID: "v1", ContentType: "application/x-npy"}
			roots = append(roots, node.Params.ArtifactID)
		}
	}
	if len(roots) != 2 {
		t.Fatal("expected two recovered roots")
	}
	probe := &renderCoordinatorProbe{}
	coordinator := RenderBatchCoordinator{Store: store, Inputs: RenderDraftCoordinator{Inputs: probe, Transport: probe, Resolver: v2draft.ClosureResolver{Catalog: catalog, Objects: catalog}}}
	second := catalog.objects[roots[1]]
	delete(catalog.objects, roots[1])
	if err := coordinator.RecoverInputs(ctx, evidence.Tenant, evidence.Batch, evidence.Actor); err == nil || len(probe.events) != 0 {
		t.Fatal("missing late input crossed publication boundary", err, probe.events)
	}
	catalog.objects[roots[1]] = second
	if err := coordinator.RecoverInputs(ctx, evidence.Tenant, evidence.Batch, evidence.Actor); err != nil || strings.Join(probe.events, ",") != "publish,register,publish,register" {
		t.Fatal("cold coordinator recovery", err, probe.events)
	}
	// Registration failure preserves the persisted draft and permits a complete replay.
	probe.events = nil
	probe.fail = "register"
	if err := coordinator.RecoverInputs(ctx, evidence.Tenant, evidence.Batch, evidence.Actor); !errors.Is(err, v2input.ErrExpired) || strings.Join(probe.events, ",") != "publish,register" {
		t.Fatal("registration failure crossed next part", err, probe.events)
	}
	probe.events = nil
	probe.fail = ""
	if err := coordinator.RecoverInputs(ctx, evidence.Tenant, evidence.Batch, evidence.Actor); err != nil || strings.Join(probe.events, ",") != "publish,register,publish,register" {
		t.Fatal("recovery replay after registration failure", err, probe.events)
	}
	// Real durable document reservation/completion and SQL input sealing.
	objects := &typedRecoveryObjects{objects: map[string]objectstore.Object{}}
	bucket := "test"
	if raw := os.Getenv("PHOTO_RENDER_OBJECT_CONFIG"); raw != "" {
		var config objectstore.Config
		if json.Unmarshal([]byte(raw), &config) != nil {
			t.Fatal("invalid object fixture configuration")
		}
		client, err := objectstore.New(config)
		if err != nil {
			t.Fatal("object fixture unavailable")
		}
		objects.delegate = client
		bucket = config.Bucket
	}
	documents := artifactjson.Publisher{Repository: typedRecoveryRepository{artifactjson.SQLRepository{DB: pool, Bucket: bucket}}, Objects: objects}
	firstSaved, err := store.LoadBatchPart(ctx, evidence.Tenant, evidence.Batch, evidence.Actor, 0)
	if err != nil {
		t.Fatal(err)
	}
	firstNodes, err := DecodeRecoveredRenderPlan(firstSaved)
	if err != nil {
		t.Fatal(err)
	}
	foreign := artifactjson.Scope{Tenant: uuid.New(), Run: firstSaved.RunID, Owner: firstNodes[0].Node.TaskID, Purpose: "v2_input_chunk_0"}
	if _, err := documents.Publish(ctx, foreign, []byte(`{"schema_version":1}`)); !errors.Is(err, artifactjson.ErrStale) || objects.puts != 0 {
		t.Fatal("foreign tenant publication accepted", err, objects.puts)
	}
	coordinator.Inputs.Transport = RenderTransportPublisher{Documents: documents}
	coordinator.Inputs.Inputs = RenderInputAdmission{DB: pool}
	apiCfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	apiCfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, apiCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	authority, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer authority.Close()
	if err := authority.QueryRow(ctx, "SELECT auth_epoch FROM photo.users WHERE id=$1", evidence.Actor.UserID).Scan(&evidence.Actor.Epoch); err != nil {
		t.Fatal(err)
	}
	// Use exact SQL object coordinates for the controlled document bodies.
	for id, obj := range catalog.objects {
		var key string
		if err := authority.QueryRow(ctx, "SELECT object_key FROM photo.artifacts WHERE tenant_id=$1 AND id=$2", evidence.Tenant, id).Scan(&key); err != nil {
			t.Fatal(err)
		}
		if body, ok := catalog.bodies[obj.Key]; ok {
			catalog.bodies[key] = body
		}
	}
	var sources v2input.SessionAnalysisObjectReader = catalog
	sourceBucket := "test"
	if objects.delegate != nil {
		reader, ok := objects.delegate.(v2input.SessionAnalysisObjectReader)
		if !ok {
			t.Fatal("live store has no source reader")
		}
		sources = reader
		sourceBucket = bucket
	}
	factory := SQLRenderBatchFactory{API: api, Scheduler: pool, Sources: sources, Documents: objects, SourceBucket: sourceBucket, DocumentBucket: bucket}
	badFactory := factory
	badFactory.API, badFactory.Scheduler = pool, api
	if _, err := badFactory.ForProject(ctx, evidence.Project); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("swapped pool roles accepted", err)
	}
	coordinator, err = factory.ForProject(ctx, evidence.Project)
	if err != nil {
		t.Fatal("build SQL coordinator", err)
	}
	if _, err := authority.Exec(ctx, "UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1", roots[1]); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.RecoverInputs(ctx, evidence.Tenant, evidence.Batch, evidence.Actor); err == nil || objects.puts != 0 {
		t.Fatal("expired second SQL root published first part", err, objects.puts)
	}
	if _, err := authority.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", roots[1]); err != nil {
		t.Fatal(err)
	}
	recoveryJob := SQLRenderBatchRecovery{Factory: factory, Timeout: 15 * time.Second}
	blocker, err := authority.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lockKey := "v26-render-recovery:" + evidence.Tenant.String() + ":" + evidence.Batch.String()
	var held bool
	if err := blocker.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtextextended($1,0))", lockKey).Scan(&held); err != nil || !held {
		t.Fatal("hold recovery lock", err)
	}
	if done, err := recoveryJob.Recover(ctx, evidence.Tenant, evidence.Batch, evidence.Project, evidence.Actor); err != nil || done || objects.puts != 0 {
		t.Fatal("contending recovery published", err, done, objects.puts)
	}
	if err := blocker.QueryRow(ctx, "SELECT pg_advisory_unlock(hashtextextended($1,0))", lockKey).Scan(&held); err != nil || !held {
		t.Fatal("release recovery blocker", err)
	}
	blocker.Release()
	verifyRenderRecoveryCancellation(t, ctx, factory, authority, evidence)
	if objects.puts != 0 {
		t.Fatal("cancelled or contending source read published documents", objects.puts)
	}
	objects.failAfterPut = true
	if done, err := recoveryJob.Recover(ctx, evidence.Tenant, evidence.Batch, evidence.Project, evidence.Actor); !errors.Is(err, io.ErrUnexpectedEOF) || done || objects.puts != 1 {
		t.Fatal("upload/completion interruption not injected", err, done, objects.puts)
	}
	blocker, err = authority.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := blocker.QueryRow(ctx, "SELECT pg_try_advisory_lock(hashtextextended($1,0))", lockKey).Scan(&held); err != nil || !held {
		t.Fatal("failed recovery leaked lock", err)
	}
	if err := blocker.QueryRow(ctx, "SELECT pg_advisory_unlock(hashtextextended($1,0))", lockKey).Scan(&held); err != nil || !held {
		t.Fatal("release failed-recovery probe", err)
	}
	blocker.Release()
	admin, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(admin.Close)
	var pending, ready int
	if err := admin.QueryRow(ctx, `SELECT count(*) FILTER(WHERE d.state='pending'),count(a.id) FROM photo.backend_documents d LEFT JOIN photo.artifacts a ON a.tenant_id=d.tenant_id AND a.id=d.artifact_id WHERE d.tenant_id=$1 AND d.owner_id=$2`, evidence.Tenant, firstNodes[0].Node.TaskID).Scan(&pending, &ready); err != nil || pending != 1 || ready != 0 {
		t.Fatal("interrupted upload not retained pending", err, pending, ready)
	}
	if done, err := recoveryJob.ProcessOne(ctx); err != nil || !done {
		t.Fatal("automatic SQL input recovery job", err, done)
	}
	if done, err := recoveryJob.ProcessOne(ctx); err != nil || done {
		t.Fatal("sealed batch selected again", err, done)
	}
	puts := objects.puts
	if puts != 4 || objects.recovers != 1 {
		t.Fatal("expected four publications and one existing-object recovery", puts, objects.recovers)
	}
	if err := coordinator.RecoverInputs(ctx, evidence.Tenant, evidence.Batch, evidence.Actor); err != nil || objects.puts != puts {
		t.Fatal("SQL input recovery replay uploaded again", err, objects.puts)
	}
	for ordinal := range evidence.Parts {
		saved, err := store.LoadBatchPart(ctx, evidence.Tenant, evidence.Batch, evidence.Actor, ordinal)
		if err != nil {
			t.Fatal(err)
		}
		nodes, err := DecodeRecoveredRenderPlan(saved)
		if err != nil {
			t.Fatal(err)
		}
		var sealed bool
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", evidence.Tenant.String())
		if err == nil {
			err = tx.QueryRow(ctx, "SELECT sealed FROM photo.v26_render_input_state($1,$2,$3)", evidence.Tenant, saved.RunID, nodes[0].Node.TaskID).Scan(&sealed)
		}
		rollbackErr := tx.Rollback(ctx)
		if err != nil || rollbackErr != nil || !sealed {
			t.Fatal("unsealed recovered inputs", err, rollbackErr)
		}
		// Structural readiness repeats actual frozen-plan/task/closure checks;
		// operation/provider qualification still controls runnable graph sealing.
		var structurallyReady bool
		if err := admin.QueryRow(ctx, "SELECT photo.v26_render_activation_ready($1,$2)", evidence.Tenant, saved.RunID).Scan(&structurallyReady); err != nil || !structurallyReady {
			t.Fatal("registered draft not structurally ready", err)
		}
		var activated bool
		if err := pool.QueryRow(ctx, "SELECT photo.seal_v2_graph($1,$2)", evidence.Tenant, saved.RunID).Scan(&activated); err != nil || activated {
			t.Fatal("unqualified render graph activated", err, activated)
		}
	}
	if err := store.ActivateBatch(ctx, evidence.Tenant, evidence.Batch, evidence.Actor); err == nil || domain.AsError(err).Code != "RENDER_UNAVAILABLE" {
		t.Fatal("unqualified batch activation", err)
	}
	var sealedChildren int
	if err := admin.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE tenant_id=$1 AND graph_sealed AND id=ANY($2::uuid[])", evidence.Tenant, []uuid.UUID{evidence.Parts[0].RunID, evidence.Parts[1].RunID}).Scan(&sealedChildren); err != nil || sealedChildren != 0 {
		t.Fatal("unqualified batch partly activated", err, sealedChildren)
	}
	var execution struct {
		Backend    string `json:"backend"`
		RuntimeSHA string `json:"render_runtime_sha"`
		TileSize   int    `json:"tile_size"`
		AlphaMode  string `json:"alpha_mode"`
	}
	if err := json.Unmarshal(firstNodes[0].Node.Profile, &execution); err != nil {
		t.Fatal(err)
	}
	choice := RenderExecutionChoice{Backend: execution.Backend, RuntimeSHA: execution.RuntimeSHA, TileSize: execution.TileSize, AlphaMode: execution.AlphaMode}
	restoreQualification, _ := seedRenderQualificationMetadata(t, ctx, admin, evidence, firstNodes[0].Evidence.Recipe.Scalars.ModelLockSHA, choice)
	// Reject the second part after the first part has passed every live gate.
	// The store's transaction must roll back the earlier seal and its side effects.
	if _, err := admin.Exec(ctx, "UPDATE photo.tasks SET cancel_requested=true WHERE tenant_id=$1 AND run_id=$2", evidence.Tenant, evidence.Parts[1].RunID); err != nil {
		t.Fatal(err)
	}
	if err := store.ActivateBatch(ctx, evidence.Tenant, evidence.Batch, evidence.Actor); err == nil || domain.AsError(err).Code != "RENDER_UNAVAILABLE" {
		t.Fatal("late child rejection not propagated", err)
	}
	var queuedEvents int
	if err := admin.QueryRow(ctx, `SELECT
 (SELECT count(*) FROM photo.runs WHERE tenant_id=$1 AND graph_sealed AND id=ANY($2::uuid[])),
 (SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=ANY($2::uuid[]) AND type='v2.run.queued')`, evidence.Tenant, []uuid.UUID{evidence.Parts[0].RunID, evidence.Parts[1].RunID}).Scan(&sealedChildren, &queuedEvents); err != nil || sealedChildren != 0 || queuedEvents != 0 {
		t.Fatal("late refusal committed partial activation", err, sealedChildren, queuedEvents)
	}
	if _, err := admin.Exec(ctx, "UPDATE photo.tasks SET cancel_requested=false WHERE tenant_id=$1 AND run_id=$2", evidence.Tenant, evidence.Parts[1].RunID); err != nil {
		t.Fatal(err)
	}
	activation, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer activation.Rollback(ctx)
	var activated bool
	if err := activateRenderBatchInTransaction(ctx, activation, evidence.Tenant, evidence.Batch, evidence.Actor); err != nil {
		t.Fatal("qualified atomic batch activation", err)
	}
	// Fixture login owns trusted roles; inspect uncommitted state without
	// granting table access to the actual scheduler service role.
	if _, err := activation.Exec(ctx, "SET LOCAL ROLE photo_fn_owner"); err != nil {
		t.Fatal(err)
	}
	if err := activation.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE tenant_id=$1 AND graph_sealed AND status='queued' AND id=ANY($2::uuid[])", evidence.Tenant, []uuid.UUID{evidence.Parts[0].RunID, evidence.Parts[1].RunID}).Scan(&sealedChildren); err != nil || sealedChildren != 2 {
		t.Fatal("batch did not seal every child", err, sealedChildren)
	}
	var queued, sealed bool
	if err := activation.QueryRow(ctx, `SELECT r.status='queued',r.graph_sealed FROM photo.runs r WHERE r.tenant_id=$1 AND r.id=$2`, evidence.Tenant, firstSaved.RunID).Scan(&queued, &sealed); err != nil || !queued || !sealed {
		t.Fatal("activated graph state", err)
	}
	var readyTasks int
	if err := activation.QueryRow(ctx, "SELECT count(*) FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2 AND state='ready' AND attempt=0", evidence.Tenant, firstSaved.RunID).Scan(&readyTasks); err != nil || readyTasks != len(firstNodes) {
		t.Fatal("activated root tasks", err, readyTasks)
	}
	var revisionBefore, revisionAfter int64
	var eventsBefore, eventsAfter, outboxBefore, outboxAfter int64
	var fixtureRole string
	if err := admin.QueryRow(ctx, "SELECT current_user").Scan(&fixtureRole); err != nil {
		t.Fatal(err)
	}
	fixtureReadRole := "SET LOCAL ROLE " + pgx.Identifier{fixtureRole}.Sanitize()
	const activationState = `SELECT revision,
 (SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2::uuid AND type='v2.run.queued'),
 (SELECT count(*) FROM photo.outbox WHERE tenant_id=$1 AND routing_key='v2.run.queued' AND payload->>'run_id'=$2::uuid::text)
 FROM photo.runs WHERE tenant_id=$1 AND id=$2::uuid`
	// Read fixture-only event/outbox evidence as the database-owning login.
	if _, err := activation.Exec(ctx, fixtureReadRole); err != nil {
		t.Fatal(err)
	}
	if err := activation.QueryRow(ctx, activationState, evidence.Tenant, firstSaved.RunID).Scan(&revisionBefore, &eventsBefore, &outboxBefore); err != nil || eventsBefore != 1 || outboxBefore != 1 {
		t.Fatal(err)
	}
	if _, err := activation.Exec(ctx, "SET LOCAL ROLE photo_scheduler"); err != nil {
		t.Fatal(err)
	}
	if err := activation.QueryRow(ctx, "SELECT photo.seal_v2_graph($1,$2)", evidence.Tenant, firstSaved.RunID).Scan(&activated); err != nil || !activated {
		t.Fatal("sealed graph replay rejected", err, activated)
	}
	if _, err := activation.Exec(ctx, fixtureReadRole); err != nil {
		t.Fatal(err)
	}
	if err := activation.QueryRow(ctx, activationState, evidence.Tenant, firstSaved.RunID).Scan(&revisionAfter, &eventsAfter, &outboxAfter); err != nil || revisionAfter != revisionBefore || eventsAfter != eventsBefore || outboxAfter != outboxBefore {
		t.Fatal("sealed replay changed revision/events/outbox", err)
	}
	if err := activation.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	restoreQualification()
	// A delayed failure report must not defer a batch whose inputs are now sealed.
	var recorded bool
	if err := pool.QueryRow(ctx, "SELECT photo.record_v26_render_recovery($1,$2,true)", evidence.Tenant, evidence.Batch).Scan(&recorded); err != nil || !recorded {
		t.Fatal("record late failure", err)
	}
	var failureCount int
	var clear bool
	if err := admin.QueryRow(ctx, "SELECT v26_input_recovery_failures,v26_input_recovery_after IS NULL FROM photo.runs WHERE id=$1", evidence.Batch).Scan(&failureCount, &clear); err != nil || failureCount != 0 || !clear {
		t.Fatal("late failure delayed sealed batch", err, failureCount, clear)
	}
	if err := store.CancelBatch(ctx, evidence.Tenant, evidence.Batch, evidence.Actor); err != nil {
		t.Fatal("cancel before recovery", err)
	}
	probe.events = nil
	if err := coordinator.RecoverInputs(ctx, evidence.Tenant, evidence.Batch, evidence.Actor); !errors.Is(err, v2input.ErrExpired) || len(probe.events) != 0 {
		t.Fatal("cancelled batch crossed publication boundary", err, probe.events)
	}
}

// Controlled immutable object adapter: validates actual published bytes, while
// durable reservations/completion and task input registration use PostgreSQL.
// Hold the owner inside object IO after its real SQL scope check and advisory
// lock acquisition. No SQL transaction is held by this injected source read.
type cancelledRenderSource struct{ entered chan struct{} }

func (s cancelledRenderSource) Open(ctx context.Context, _ objectstore.Object) (io.ReadCloser, error) {
	select {
	case s.entered <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func verifyRenderRecoveryCancellation(t *testing.T, ctx context.Context, factory SQLRenderBatchFactory, authority *pgxpool.Pool, evidence renderBatchRestartEvidence) {
	t.Helper()
	entered := make(chan struct{}, 1)
	ownerFactory := factory
	ownerFactory.Sources = cancelledRenderSource{entered: entered}
	owner := SQLRenderBatchRecovery{Factory: ownerFactory, Timeout: 15 * time.Second}
	competitor := SQLRenderBatchRecovery{Factory: factory, Timeout: 15 * time.Second}
	ownerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	type outcome struct {
		done bool
		err  error
	}
	finished := make(chan outcome, 1)
	go func() {
		done, err := owner.Recover(ownerCtx, evidence.Tenant, evidence.Batch, evidence.Project, evidence.Actor)
		finished <- outcome{done, err}
	}()
	defer func() {
		cancel()
		select {
		case <-finished:
		case <-time.After(6 * time.Second):
			t.Error("cancelled recovery did not join")
		}
	}()
	select {
	case <-entered:
	case result := <-finished:
		finished <- result
		t.Fatal("owner did not reach scoped source IO", result.err)
	case <-ctx.Done():
		t.Fatal("source gate timeout", ctx.Err())
	}
	key := "v26-render-recovery:" + evidence.Tenant.String() + ":" + evidence.Batch.String()
	var lockCount int
	var outsideTransaction bool
	if err := authority.QueryRow(ctx, `SELECT count(*),coalesce(bool_and(a.xact_start IS NULL AND a.state='idle'),false)
 FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid
 WHERE l.locktype='advisory' AND l.granted AND l.objsubid=1
 AND l.classid::bigint=((hashtextextended($1,0)>>32)&4294967295)
 AND l.objid::bigint=(hashtextextended($1,0)&4294967295)`, key).Scan(&lockCount, &outsideTransaction); err != nil || lockCount != 1 || !outsideTransaction {
		t.Fatal("source IO lock must be held outside SQL transaction", lockCount, outsideTransaction, err)
	}
	if done, err := competitor.Recover(ctx, evidence.Tenant, evidence.Batch, evidence.Project, evidence.Actor); err != nil || done {
		t.Fatal("second scheduler entered owned batch", done, err)
	}
	cancel()
	select {
	case result := <-finished:
		finished <- result // Deferred join observes the already completed owner.
		if result.done || !errors.Is(result.err, context.Canceled) {
			t.Fatal("source cancellation outcome", result.done, result.err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("owner cancellation timeout")
	}
}

type typedRecoveryObjects struct {
	objects      map[string]objectstore.Object
	puts         int
	delegate     artifactjson.Objects
	failAfterPut bool
	recovers     int
}

func (o *typedRecoveryObjects) PutImmutable(ctx context.Context, key string, r io.Reader, size int64, digest, mime string) (objectstore.Object, error) {
	if o.delegate != nil {
		obj, err := o.delegate.PutImmutable(ctx, key, r, size, digest, mime)
		if err == nil {
			o.puts++
			if o.failAfterPut {
				o.failAfterPut = false
				return objectstore.Object{}, io.ErrUnexpectedEOF
			}
		}
		return obj, err
	}
	if err := ctx.Err(); err != nil {
		return objectstore.Object{}, err
	}
	if _, ok := o.objects[key]; ok {
		return objectstore.Object{}, objectstore.ErrExists
	}
	body, err := io.ReadAll(io.LimitReader(r, size+1))
	if err != nil {
		return objectstore.Object{}, err
	}
	sum := sha256.Sum256(body)
	if int64(len(body)) != size || hex.EncodeToString(sum[:]) != digest {
		return objectstore.Object{}, objectstore.ErrIntegrity
	}
	obj := objectstore.Object{Key: key, VersionID: "v1", Bytes: size, SHA256: digest, ContentType: mime}
	o.objects[key] = obj
	o.puts++
	if o.failAfterPut {
		o.failAfterPut = false
		return objectstore.Object{}, io.ErrUnexpectedEOF
	}
	return obj, nil
}
func (o *typedRecoveryObjects) RecoverImmutable(ctx context.Context, key string, size int64, digest, mime string) (objectstore.Object, error) {
	o.recovers++
	if o.delegate != nil {
		return o.delegate.RecoverImmutable(ctx, key, size, digest, mime)
	}
	if err := ctx.Err(); err != nil {
		return objectstore.Object{}, err
	}
	obj, ok := o.objects[key]
	if !ok || obj.Bytes != size || obj.SHA256 != digest || obj.ContentType != mime {
		return objectstore.Object{}, objectstore.ErrIntegrity
	}
	return obj, nil
}

type typedRecoveryRepository struct{ artifactjson.SQLRepository }

func (r typedRecoveryRepository) Reserve(ctx context.Context, s artifactjson.Scope, digest string, size int64) (artifactjson.Reservation, error) {
	v, e := r.SQLRepository.Reserve(ctx, s, digest, size)
	if e != nil {
		return v, fmt.Errorf("reserve %s: %w", s.Purpose, e)
	}
	return v, nil
}
func (r typedRecoveryRepository) Complete(ctx context.Context, s artifactjson.Scope, ticket artifactjson.Reservation, obj objectstore.Object) (artifactjson.Ref, error) {
	v, e := r.SQLRepository.Complete(ctx, s, ticket, obj)
	if e != nil {
		return v, fmt.Errorf("complete %s: %w", s.Purpose, e)
	}
	return v, nil
}

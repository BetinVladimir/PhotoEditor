package v2render

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
	"time"
)

func TestIntegrationRenderBillingAdmission(t *testing.T) {
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

	holder, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer holder.Rollback(context.Background())
	if _, err = holder.Exec(ctx, `SELECT 1 FROM photo.billing_resource_balances WHERE tenant_id=$1 FOR UPDATE`, f.Tenant); err != nil {
		t.Fatal(err)
	}
	if err = store.BeginBatch(ctx, f.Tenant, f.Batch, f.Project, f.Snapshot, who, strings.Repeat("a", 64), parts); domain.AsError(err).Code != "BILLING_UNAVAILABLE" {
		t.Fatal("cold admission deadline", err)
	}
	if err = holder.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = store.BeginBatch(ctx, f.Tenant, f.Batch, f.Project, f.Snapshot, who, strings.Repeat("a", 64), parts); err != nil {
		t.Fatal("billing staging", err)
	}
	done := make(chan error, 6)
	for i := 0; i < 6; i++ {
		go func() {
			done <- store.BeginBatch(ctx, f.Tenant, f.Batch, f.Project, f.Snapshot, who, strings.Repeat("a", 64), parts)
		}()
	}
	for i := 0; i < 6; i++ {
		if e := <-done; e != nil {
			t.Fatal("concurrent replay", e)
		}
	}
	var requests, tickets, frames int
	if err = admin.QueryRow(ctx, `SELECT (SELECT count(*) FROM photo.billing_usage_requests WHERE tenant_id=$1),(SELECT count(*) FROM photo.billing_usage_tickets WHERE tenant_id=$1),(SELECT count(*) FROM photo.billing_render_frames WHERE tenant_id=$1)`, f.Tenant).Scan(&requests, &tickets, &frames); err != nil || requests != 1 || tickets != 1 || frames != 1 {
		t.Fatal("duplicate funding", requests, tickets, frames, err)
	}
	other := uuid.New()
	otherParts, err := CompileRenderBatchPlans(other, f.Project, f.Snapshot, []RenderDraftRequest{request}, [][]v2input.Entry{entries})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.BeginBatch(ctx, f.Tenant, other, f.Project, f.Snapshot, who, strings.Repeat("a", 64), otherParts); domain.AsError(err).Code != "PHOTO_INITIAL_IN_PROGRESS" {
		t.Fatal("independent initial", err)
	}
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM photo.runs WHERE id=$1`, other).Scan(&requests); err != nil || requests != 0 {
		t.Fatal("quota refusal leaked run", requests, err)
	}
	// Real cancellation/finalizer entrypoints, with the blocked task terminalized by scheduler metadata.
	var ok bool
	if err = pool.QueryRow(ctx, `SELECT photo.cancel_v26_render_batch($1,$2,$3,$4)`, f.Tenant, f.Batch, who.UserID, who.SessionID).Scan(&ok); err != nil || !ok {
		t.Fatal("cancel", err)
	}
	if _, err = admin.Exec(ctx, `UPDATE photo.tasks SET state='cancelled',terminal_reason='fixture_cancel' WHERE tenant_id=$1 AND run_id=$2`, f.Tenant, parts[0].RunID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if _, err = pool.Exec(ctx, `SELECT photo.finalize_run_cancellation()`); err != nil {
			t.Fatal(err)
		}
	}
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM photo.billing_usage_tickets WHERE tenant_id=$1 AND state='accepted'`, f.Tenant).Scan(&tickets); err != nil || tickets != 1 {
		t.Fatal("generic Apply settler touched render funding", tickets, err)
	}
	if err = pool.QueryRow(ctx, `SELECT photo.billing_settle_next_render()`).Scan(&ok); err != nil || !ok {
		t.Fatal("settlement", ok, err)
	}
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM photo.billing_usage_tickets WHERE tenant_id=$1 AND state='released'`, f.Tenant).Scan(&tickets); err != nil || tickets != 1 {
		t.Fatal("render refund", tickets, err)
	}
	standalone := uuid.New()
	plan, err := CompileRenderRunPlan(standalone, f.Project, f.Snapshot, []RenderDraftRequest{request}, [][]v2input.Entry{entries})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Begin(ctx, f.Tenant, standalone, who, strings.Repeat("a", 64), plan); err != nil {
		t.Fatal("standalone begin", err)
	}
	if err = store.Begin(ctx, f.Tenant, standalone, who, strings.Repeat("a", 64), plan); err != nil {
		t.Fatal("standalone replay", err)
	}
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM photo.billing_usage_requests WHERE tenant_id=$1`, f.Tenant).Scan(&requests); err != nil || requests != 2 {
		t.Fatal("standalone duplicate", requests, err)
	}
	if _, err = admin.Exec(ctx, `UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1`, who.SessionID); err != nil {
		t.Fatal(err)
	}
	if err = store.BeginBatch(ctx, f.Tenant, f.Batch, f.Project, f.Snapshot, who, strings.Repeat("a", 64), parts); err == nil {
		t.Fatal("revoked replay")
	}
}

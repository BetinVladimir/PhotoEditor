package run

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"sync"
	"testing"
	"time"
)

// Relational admission test: private mode represents the already compiled trusted
// closure. Pixel execution and the public compiler are qualified separately.
func TestIntegrationBillingEditorAdmission(t *testing.T) {
	raw := os.Getenv("PHOTO_BILLING_EDITOR_FIXTURE")
	if raw == "" {
		t.Skip("isolated Editor fixture required")
	}
	var f struct {
		Tenant, Actor, Session, Project, Root uuid.UUID
		Epoch                                 int64
	}
	if json.Unmarshal([]byte(raw), &f) != nil {
		t.Fatal("invalid fixture")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
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
	hash, err := canonical.Hash(json.RawMessage(identityStyleJSON))
	if err != nil {
		t.Fatal(err)
	}
	var artifact, style, version uuid.UUID
	if err = admin.QueryRow(ctx, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,state,verified_at) VALUES($1,'photo',$2,'v1',$3,$4,'application/json','style','ready',now()) RETURNING id`, f.Tenant, uuid.NewString(), hash, len(identityStyleJSON)).Scan(&artifact); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.styles(tenant_id,project_id,name,created_by) VALUES($1,$2,'Billing identity',$3) RETURNING id`, f.Tenant, f.Project, f.Actor).Scan(&style); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.style_versions(tenant_id,style_id,version_no,config_json,config_hash,schema_version,renderer_version,approval_status,metrics,export_artifact_id) VALUES($1,$2,1,$3::jsonb,$4,'2.0.0','photo_renderer_v2','accepted','{}',$5) RETURNING id`, f.Tenant, style, string(identityStyleJSON), hash, artifact).Scan(&version); err != nil {
		t.Fatal(err)
	}

	svc := Service{DB: api}
	who := auth.Identity{UserID: f.Actor, SessionID: f.Session, Epoch: f.Epoch}
	m := &editorEffectApplyMode{ProjectID: f.Project, ExecutedStyle: v2input.Ref{ArtifactID: artifact, SHA256: hash}}
	err = admin.QueryRow(ctx, `SELECT asset_version_id,source_artifact_id,source_sha,source_canvas_hash,frozen_recipe_artifact_id,frozen_recipe_sha,frozen_document_id,draft_document_id,draft_revision,compilation_sha FROM photo.editor_effect_builds WHERE run_id=$1`, f.Root).Scan(&m.Binding.AssetVersionID, &m.Binding.Source.ArtifactID, &m.Binding.Source.SHA256, &m.Binding.CanvasHash, &m.FrozenRecipe.ArtifactID, &m.FrozenRecipe.SHA256, &m.FrozenDocumentID, &m.DraftDocumentID, &m.DraftRevision, &m.CompilationSHA)
	if err != nil {
		t.Fatal(err)
	}
	m.ItemID = uuid.NewSHA1(m.FrozenDocumentID, []byte("editor-effect-source"))
	m.SourceSHA = m.Binding.Source.SHA256
	m.Closure = []v2input.Ref{m.Binding.Source, m.FrozenRecipe, m.ExecutedStyle}
	m.Override, err = v2draft.BuildAnalysisOverride([]byte(identityStyleJSON), m.SourceSHA, v2draft.CorrectionVector{})
	if err != nil {
		t.Fatal(err)
	}
	m.Intent = EditorEffectRequest{Source: m.Binding, Recipe: m.FrozenRecipe, FrozenDocumentID: m.FrozenDocumentID, DraftDocumentID: m.DraftDocumentID, DraftRevision: m.DraftRevision}
	req := ApplyRequest{SourceRevision: 1, StyleVersionID: version, Scope: "selected", ItemIDs: []uuid.UUID{m.ItemID}}
	key := uuid.NewString()
	route := "Editor.Effects." + f.Project.String()
	// Cold admission must bound lock waits before taking domain locks.
	blocker, err := admin.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = blocker.Exec(ctx, `SELECT id FROM photo.tenants WHERE id=$1 FOR UPDATE`, f.Tenant); err != nil {
		t.Fatal(err)
	}
	_, err = svc.apply(ctx, who, f.Tenant, uuid.Nil, key, req, false, route, "", nil, nil, m)
	blocker.Rollback(ctx)
	if domain.AsError(err).Code != "BILLING_UNAVAILABLE" {
		t.Fatal("cold admission must map deadline to BILLING_UNAVAILABLE", err)
	}
	first, err := svc.apply(ctx, who, f.Tenant, uuid.Nil, key, req, false, route, "", nil, nil, m)
	if err != nil {
		t.Fatal("fresh editor", err)
	}
	if first.Status != 202 {
		t.Fatal(first.Status)
	}
	var accepted ApplyAccepted
	if json.Unmarshal(first.Body, &accepted) != nil {
		t.Fatal("receipt")
	}
	var wg sync.WaitGroup
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := svc.apply(ctx, who, f.Tenant, uuid.Nil, key, req, false, route, "", nil, nil, m)
			if e != nil || !bytes.Equal(r.Body, first.Body) {
				t.Error("replay", e)
			}
		}()
	}
	wg.Wait()
	// Public accepted replay survives absent fresh renderer/object dependencies.
	public := EditorEffectsService{Runs: svc}
	replay, replayErr := public.Begin(ctx, who, f.Tenant, f.Project, key, m.Intent)
	if replayErr != nil || !bytes.Equal(replay.Body, first.Body) {
		t.Fatal("public accepted replay", replayErr)
	}
	changed := m.Intent
	changed.DraftRevision++
	if _, e := public.Begin(ctx, who, f.Tenant, f.Project, key, changed); domain.AsError(e).Code != "IDEMPOTENCY_CONFLICT" {
		t.Fatal("public replay conflict", e)
	}
	var count int
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM photo.billing_usage_requests WHERE tenant_id=$1`, f.Tenant).Scan(&count); err != nil || count != 1 {
		t.Fatal("request count", count, err)
	}
	var frozen bool
	if err = admin.QueryRow(ctx, `SELECT s.expires_at IS NULL AND s.item_count=1 AND (SELECT count(*) FROM photo.snapshot_items i WHERE i.snapshot_id=s.id)=1 FROM photo.runs r JOIN photo.snapshots s ON s.id=r.input_snapshot_id WHERE r.id=$1`, accepted.RunID).Scan(&frozen); err != nil || !frozen {
		t.Fatal("snapshot", frozen, err)
	}
	// An independent initial request for the same original cannot reserve again.
	if _, err = svc.apply(ctx, who, f.Tenant, uuid.Nil, uuid.NewString(), req, false, route, "", nil, nil, m); err == nil {
		t.Fatal("duplicate initial accepted")
	}
	if err = admin.QueryRow(ctx, `SELECT count(*) FROM photo.editor_effect_builds WHERE tenant_id=$1`, f.Tenant).Scan(&count); err != nil || count != 2 {
		t.Fatal("rollback leaked build", count, err)
	}
	if _, err = admin.Exec(ctx, `UPDATE photo.sessions SET revoked_at=now() WHERE id=$1`, f.Session); err != nil {
		t.Fatal(err)
	}
	if _, e := public.Begin(ctx, who, f.Tenant, f.Project, key, m.Intent); e == nil {
		t.Fatal("revoked accepted replay")
	}

}

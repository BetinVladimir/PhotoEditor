package v2draft

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
	"time"
)

func TestIntegrationPortableBackgroundAsset(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()
	var user, tenant, project, style, version uuid.UUID
	var epoch int64
	if err = admin.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", uuid.NewString()+"@test.example").Scan(&user, &epoch); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "SELECT id FROM photo.tenants WHERE owner_user_id=$1", user).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	session := uuid.New()
	if _, err = admin.Exec(ctx, "INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')", session, user, auth.HashSecret(uuid.NewString()), uuid.New()); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.projects(tenant_id,name) VALUES($1,'Background') RETURNING id", tenant).Scan(&project); err != nil {
		t.Fatal(err)
	}
	createArtifact := func(role, media, digest string) uuid.UUID {
		t.Helper()
		var id uuid.UUID
		if err := admin.QueryRow(ctx, `INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role) VALUES($1,'test',$2,$3,100,$4,'v1',now(),$5) RETURNING id`, tenant, uuid.NewString(), digest, media, role).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	configSHA, assetSHA := strings.Repeat("a", 64), strings.Repeat("b", 64)
	config := createArtifact("style_candidate", "application/json", configSHA)
	asset := createArtifact("style_asset", "image/png", assetSHA)
	if err = admin.QueryRow(ctx, "INSERT INTO photo.styles(tenant_id,project_id,name,created_by) VALUES($1,$2,'Background',$3) RETURNING id", tenant, project, user).Scan(&style); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.style_versions(tenant_id,style_id,version_no,config_json,config_hash,schema_version,renderer_version,approval_status,metrics,export_artifact_id) VALUES($1,$2,1,'{"extensions":{"background":{"mode":"asset","asset_id":"background"}}}',$3,'2.0.0','photo_renderer_v2','accepted','{}',$4) RETURNING id`, tenant, style, configSHA, config).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "INSERT INTO photo.style_asset_bindings(tenant_id,style_version_id,portable_asset_id,artifact_id,sha256) VALUES($1,$2,'background',$3,$4)", tenant, version, asset, assetSHA); err != nil {
		t.Fatal(err)
	}
	resolver := SQLBackgroundAssetResolver{DB: api, Bucket: "test"}
	who := auth.Identity{UserID: user, SessionID: session, Epoch: epoch}
	pin := v2input.Ref{ArtifactID: config, SHA256: configSHA}
	out, err := resolver.Resolve(ctx, who, tenant, project, pin, "background")
	if err != nil {
		t.Fatal(err)
	}
	if out.Ref.ArtifactID != asset || out.Object.VersionID != "v1" || out.Object.SHA256 != assetSHA || out.StyleVersionID != version {
		t.Fatalf("lost binding: %#v", out)
	}
	if _, err = resolver.Resolve(ctx, who, tenant, uuid.New(), pin, "background"); err == nil {
		t.Fatal("foreign project accepted")
	}
	frozen, err := resolver.Freeze(ctx, who, tenant, project, pin, "background", strings.Repeat("d", 64), nil)
	if err != nil {
		t.Fatal(err)
	}
	var ready bool
	if err = admin.QueryRow(ctx, "SELECT photo.v26_render_background_binding_ready($1,$2,$3::jsonb)", tenant, project, json.RawMessage(frozen)).Scan(&ready); err != nil || !ready {
		t.Fatalf("qualified background metadata denied: %v", err)
	}
	var changed map[string]any
	if err = json.Unmarshal(frozen, &changed); err != nil {
		t.Fatal(err)
	}
	changed["expected_bytes"] = 101
	invalid, err := json.Marshal(changed)
	if err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "SELECT photo.v26_render_background_binding_ready($1,$2,$3::jsonb)", tenant, project, json.RawMessage(invalid)).Scan(&ready); err != nil || ready {
		t.Fatalf("wrong asset bytes accepted: %v", err)
	}
	compiled, err := DecodeFrozenBackgroundAsset(frozen)
	if err != nil || compiled.Source.ArtifactID != asset || compiled.Style != pin || compiled.StyleVersionID != version {
		t.Fatalf("wrong authorized freeze: %v", err)
	}
	sourceSHA := strings.Repeat("f", 64)
	workingID := createArtifact("working_image", "application/json", sourceSHA)
	var photoID, photoVersion uuid.UUID
	if err = admin.QueryRow(ctx, `INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`, tenant, project, asset).Scan(&photoID); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, `INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,'{}','accepted') RETURNING id`, tenant, photoID, workingID, sourceSHA).Scan(&photoVersion); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "INSERT INTO photo.tenant_usage(tenant_id) VALUES($1) ON CONFLICT DO NOTHING", tenant); err != nil {
		t.Fatal(err)
	}
	binding := v2input.SessionVersionBinding{AssetVersionID: photoVersion, Source: v2input.Ref{ArtifactID: workingID, SHA256: sourceSHA}, CanvasHash: strings.Repeat("a", 64)}
	authored, err := canonical.Marshal(map[string]any{"schema_version": 3, "source": binding, "base_style": pin, "session_corrections": nil, "learning_version_id": nil, "overrides": []v2input.Ref{}, "layer_stack": nil, "renderer_version": "v2.2", "parameter_delta": CorrectionVector{}})
	if err != nil {
		t.Fatal(err)
	}
	inputs := ResolvedRecipeInputs{Binding: binding, BaseStyle: pin, Style: StyleBaseline{GainsRGB: [3]float64{1, 1, 1}, Saturation: 1}, ICCLockSHA: strings.Repeat("d", 64), ModelLockSHA: strings.Repeat("e", 64)}
	store := approvalReadStore{bodies: map[string][]byte{}}
	publisher := Publisher{DB: api, Objects: store, Bucket: "test"}
	document := uuid.New()
	published, err := publisher.CompileAndPublishFrozenRenderRecipeWithBackground(ctx, who, tenant, project, document, authored, inputs, nil, "background", nil)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := publisher.CompileAndPublishFrozenRenderRecipeWithBackground(ctx, who, tenant, project, document, authored, inputs, nil, "background", nil)
	if err != nil || replay != published {
		t.Fatal("background publisher replay changed ref", err)
	}
	var registered bool
	if err = admin.QueryRow(ctx, "SELECT render_background_asset IS NOT NULL AND render_registered_at IS NOT NULL FROM photo.v2_draft_uploads WHERE tenant_id=$1 AND document_id=$2 AND artifact_id=$3 AND sha256=$4", tenant, document, published.ArtifactID, published.SHA256).Scan(&registered); err != nil || !registered {
		t.Fatal("publisher did not register background", err)
	}
	// Resolve the typed child through its registered root and live accepted style.
	parent := LayerRef{Contract: "StyleBackgroundAsset", Ref: compiled.Source, ParentRef: published, ExpectedBackground: &compiled}
	obj, err := (SQLArtifactCatalog{DB: api, Bucket: "test", ProjectID: project, AssetVersionID: photoVersion}).Lookup(ctx, who, tenant, parent)
	if err != nil || obj.SHA256 != assetSHA {
		t.Fatal("background catalog scope failed", err)
	}
	otherConfig := createArtifact("style_candidate", "application/json", strings.Repeat("e", 64))
	var otherVersion uuid.UUID
	if err = admin.QueryRow(ctx, `INSERT INTO photo.style_versions(tenant_id,style_id,version_no,config_json,config_hash,schema_version,renderer_version,approval_status,metrics,export_artifact_id) VALUES($1,$2,3,'{"extensions":{"background":{"mode":"asset","asset_id":"different"}}}',$3,'2.0.0','photo_renderer_v2','accepted','{}',$4) RETURNING id`, tenant, style, strings.Repeat("e", 64), otherConfig).Scan(&otherVersion); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "INSERT INTO photo.style_asset_bindings(tenant_id,style_version_id,portable_asset_id,artifact_id,sha256) VALUES($1,$2,'background',$3,$4)", tenant, otherVersion, asset, assetSHA); err != nil {
		t.Fatal(err)
	}
	if _, err = resolver.Resolve(ctx, who, tenant, project, v2input.Ref{ArtifactID: otherConfig, SHA256: strings.Repeat("e", 64)}, "background"); err == nil {
		t.Fatal("non-authored portable asset resolved")
	}

	draftConfig := createArtifact("style_candidate", "application/json", strings.Repeat("c", 64))
	var draftVersion uuid.UUID
	if err = admin.QueryRow(ctx, `INSERT INTO photo.style_versions(tenant_id,style_id,version_no,config_json,config_hash,schema_version,renderer_version,approval_status,metrics,export_artifact_id) VALUES($1,$2,2,'{"extensions":{"background":{"mode":"asset","asset_id":"background"}}}',$3,'2.0.0','photo_renderer_v2','needs_review','{}',$4) RETURNING id`, tenant, style, strings.Repeat("c", 64), draftConfig).Scan(&draftVersion); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, "INSERT INTO photo.style_asset_bindings(tenant_id,style_version_id,portable_asset_id,artifact_id,sha256) VALUES($1,$2,'background',$3,$4)", tenant, draftVersion, asset, assetSHA); err != nil {
		t.Fatal(err)
	}
	if _, err = resolver.Resolve(ctx, who, tenant, project, v2input.Ref{ArtifactID: draftConfig, SHA256: strings.Repeat("c", 64)}, "background"); err == nil {
		t.Fatal("unaccepted version resolved")
	}

	for _, mutation := range []string{
		"UPDATE photo.styles SET archived_at=now() WHERE id=$1",
		"UPDATE photo.artifacts SET expires_at=now()-interval '1 hour' WHERE id=$1",
	} {
		target := style
		if strings.Contains(mutation, "artifacts") {
			target = asset
		}
		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, mutation, target); err != nil {
			tx.Rollback(ctx)
			t.Fatal(err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err = resolver.Resolve(ctx, who, tenant, project, pin, "background"); err == nil {
			t.Fatal("inactive asset/style accepted")
		}
		if target == style {
			_, err = admin.Exec(ctx, "UPDATE photo.styles SET archived_at=NULL WHERE id=$1", target)
		} else {
			_, err = admin.Exec(ctx, "UPDATE photo.artifacts SET expires_at=NULL WHERE id=$1", target)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

package style

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
)

func TestIntegrationAnalyzeFreezesOnlyConfirmedPairs(t *testing.T) {
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
	cfg, _ := pgxpool.ParseConfig(dsn)
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer api.Close()

	var user, tenant, project uuid.UUID
	var epoch int64
	if err = admin.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", uuid.NewString()+"@style.test").Scan(&user, &epoch); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "SELECT id FROM photo.tenants WHERE owner_user_id=$1", user).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	session := uuid.New()
	if _, err = admin.Exec(ctx, "INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')", session, user, auth.HashSecret(uuid.NewString()), uuid.New()); err != nil {
		t.Fatal(err)
	}
	if err = admin.QueryRow(ctx, "INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'Style','workspace') RETURNING id", tenant).Scan(&project); err != nil {
		t.Fatal(err)
	}
	lock := "a" + "000000000000000000000000000000000000000000000000000000000000000"
	node := "style-" + uuid.NewString()
	if _, err = admin.Exec(ctx, "INSERT INTO photo.resource_nodes(node_id,enabled,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes,ram_available_bytes,scratch_available_bytes,reported_at,doctor_report_sha256) VALUES($1,true,8589934592,8589934592,8589934592,8589934592,8589934592,clock_timestamp(),$2)", node, lock); err != nil {
		t.Fatal(err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations) VALUES
		 ($1,$2,'["gpu"]','["cpu"]',$4,clock_timestamp(),'{}',session_user,$3,true,'{"pair.fit.color":["cpu"],"face.detect":["cpu"],"face.landmarks":["cpu"],"face.parse":["cpu"],"mask.person":["cpu"]}'),
		 ($1,$5,'["cpu"]','["cpu"]',$4,clock_timestamp(),'{}',session_user,$3,true,'{"style.aggregate":["cpu"],"style.validate":["cpu"],"style.package":["cpu"],"mask.face_skin":["cpu"],"pair.fit.beauty":["cpu"],"pair.fit.defects":["cpu"]}')`, tenant, uuid.New(), node, lock, uuid.New()); err != nil {
		t.Fatal(err)
	}

	makeStyleSet := func(confirmed []bool) (uuid.UUID, uuid.UUID, int64) {
		var style, set uuid.UUID
		var revision int64
		if err = admin.QueryRow(ctx, "INSERT INTO photo.styles(tenant_id,project_id,name,created_by) VALUES($1,$2,$3,$4) RETURNING id", tenant, project, uuid.NewString(), user).Scan(&style); err != nil {
			t.Fatal(err)
		}
		if err = admin.QueryRow(ctx, "INSERT INTO photo.training_sets(tenant_id,style_id) VALUES($1,$2) RETURNING id,revision", tenant, style).Scan(&set, &revision); err != nil {
			t.Fatal(err)
		}
		for i, ok := range confirmed {
			versions := make([]uuid.UUID, 2)
			for side := 0; side < 2; side++ {
				var artifact, asset uuid.UUID
				digest := lock
				if err = admin.QueryRow(ctx, "INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role) VALUES($1,'photo',$2,$3,$4,128,'application/json','working_image') RETURNING id", tenant, uuid.NewString(), uuid.NewString(), digest).Scan(&artifact); err != nil {
					t.Fatal(err)
				}
				if err = admin.QueryRow(ctx, "INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id", tenant, project, artifact).Scan(&asset); err != nil {
					t.Fatal(err)
				}
				if err = admin.QueryRow(ctx, "INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,'{}','accepted') RETURNING id", tenant, asset, artifact, digest).Scan(&versions[side]); err != nil {
					t.Fatal(err)
				}
			}
			if _, err = admin.Exec(ctx, "INSERT INTO photo.training_pairs(tenant_id,training_set_id,before_asset_version_id,after_asset_version_id,session_id,confirmed,included) VALUES($1,$2,$3,$4,$5,$6,true)", tenant, set, versions[0], versions[1], "session-"+uuid.NewString(), ok); err != nil {
				t.Fatal(err)
			}
			_ = i
		}
		return style, set, revision
	}
	who := auth.Identity{UserID: user, SessionID: session, Epoch: epoch}
	service := Service{DB: api}
	for _, flags := range [][]bool{{true}, {true, true, false}} {
		style, set, revision := makeStyleSet(flags)
		res, e := service.Analyze(ctx, who, tenant, style, uuid.NewString(), AnalyzeRequest{TrainingSetID: set, TrainingSetRevision: revision, AnalysisOptions: AnalysisOptions{AlignmentMode: "similarity_only", Seed: 42}})
		if e != nil {
			t.Fatal(e)
		}
		var body RunAccepted
		if json.Unmarshal(res.Body, &body) != nil || body.Status != "queued" {
			t.Fatalf("bad response %s", res.Body)
		}
		var count int
		if err = admin.QueryRow(ctx, "SELECT s.item_count FROM photo.runs r JOIN photo.snapshots s ON s.id=r.input_snapshot_id WHERE r.id=$1", body.RunID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		expected := 0
		for _, v := range flags {
			if v {
				expected++
			}
		}
		if count != expected {
			t.Fatalf("snapshot count %d want %d", count, expected)
		}
	}
	_, holdoutSet, holdoutRevision := makeStyleSet([]bool{true, true})
	holdoutResponse, e := service.CreateHoldoutSnapshot(ctx, who, tenant, holdoutSet, uuid.NewString(), HoldoutSnapshotRequest{ExpectedRevision: holdoutRevision})
	if e != nil {
		t.Fatal(e)
	}
	var holdout HoldoutSnapshot
	if json.Unmarshal(holdoutResponse.Body, &holdout) != nil || holdout.ItemCount != 2 || holdout.Digest == "" {
		t.Fatalf("bad holdout response %s", holdoutResponse.Body)
	}
	trainingStyle, trainingSet, trainingRevision := makeStyleSet([]bool{true})
	withHoldout, e := service.Analyze(ctx, who, tenant, trainingStyle, uuid.NewString(), AnalyzeRequest{TrainingSetID: trainingSet, TrainingSetRevision: trainingRevision, AnalysisOptions: AnalysisOptions{AlignmentMode: "similarity_only", Seed: 42}, HoldoutSnapshotID: &holdout.SnapshotID})
	if e != nil {
		t.Fatal(e)
	}
	var accepted RunAccepted
	if json.Unmarshal(withHoldout.Body, &accepted) != nil {
		t.Fatal("bad analyze response")
	}
	var graphVersion int
	var savedDigest string
	if err = admin.QueryRow(ctx, `SELECT b.graph_version,r.config_snapshot->>'holdout_snapshot_digest' FROM photo.style_analysis_builds b JOIN photo.runs r ON r.tenant_id=b.tenant_id AND r.id=b.run_id WHERE b.run_id=$1`, accepted.RunID).Scan(&graphVersion, &savedDigest); err != nil {
		t.Fatal(err)
	}
	if graphVersion != 3 || savedDigest != holdout.Digest {
		t.Fatalf("holdout binding missing: v%d %s", graphVersion, savedDigest)
	}
	style, set, revision := makeStyleSet([]bool{false, false})
	before := countRows(t, ctx, admin, "SELECT count(*) FROM photo.runs WHERE tenant_id=$1 AND kind='analyze'", tenant)
	_, err = service.Analyze(ctx, who, tenant, style, uuid.NewString(), AnalyzeRequest{TrainingSetID: set, TrainingSetRevision: revision, AnalysisOptions: AnalysisOptions{AlignmentMode: "similarity_only", Seed: 42}})
	if domain.AsError(err).Code != "TRAINING_PAIR_COUNT_INVALID" {
		t.Fatalf("unexpected invalid-set result %v", err)
	}
	if after := countRows(t, ctx, admin, "SELECT count(*) FROM photo.runs WHERE tenant_id=$1 AND kind='analyze'", tenant); after != before {
		t.Fatal("invalid set created analyze run")
	}
}

func countRows(t *testing.T, ctx context.Context, db *pgxpool.Pool, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(ctx, query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

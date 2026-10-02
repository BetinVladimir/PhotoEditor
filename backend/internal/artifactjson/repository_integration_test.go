package artifactjson

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
)

func TestIntegrationDocumentReservationFencing(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("database configuration failed")
	}
	defer db.Close()
	var user, tenant, snapshot, run, artifact, review uuid.UUID
	var epoch int64
	hash := strings.Repeat("a", 64)
	if err = db.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", uuid.NewString()+"@example.test").Scan(&user, &epoch); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, "SELECT id FROM photo.tenants WHERE owner_user_id=$1", user).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, "INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type) VALUES($1,'test',$2,$3,2,'application/json') RETURNING id", tenant, uuid.NewString(), hash).Scan(&artifact); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, "INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'input','user',$2,$3,1) RETURNING id", tenant, user, hash).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, "INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES($1,'apply','running',$2,'{}',$3,$4) RETURNING id", tenant, snapshot, hash, user).Scan(&run); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, "INSERT INTO photo.reviews(tenant_id,run_id,candidate_artifact_id,candidate_hash,kind,reasons) VALUES($1,$2,$3,$4,'defects','[]') RETURNING id", tenant, run, artifact, hash).Scan(&review); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, "UPDATE photo.reviews SET state='decided',revision=2,decision='accept',decided_by=$2,decided_at=now(),decision_hash=$3,selection='{}' WHERE id=$1", review, user, hash); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("invalid database configuration")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("scheduler database unavailable")
	}
	defer pool.Close()
	repo := SQLRepository{DB: pool, Bucket: "test-bucket"}
	scope := Scope{tenant, run, review, "review_selection"}
	first, err := repo.Reserve(ctx, scope, hash, 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := repo.Reserve(ctx, scope, hash, 2)
	if err != nil || second.ArtifactID != first.ArtifactID || second.Token == first.Token {
		t.Fatal("reservation resume mismatch", err)
	}
	key, _ := DocumentKey(scope, first.ArtifactID)
	object := objectstore.Object{Key: key, VersionID: "version-test", SHA256: hash, Bytes: 2, ContentType: "application/json"}
	if _, err = repo.Complete(ctx, scope, first, object); !errors.Is(err, ErrStale) {
		t.Fatal("stale token accepted", err)
	}
	if _, err = repo.Reserve(ctx, scope, strings.Repeat("b", 64), 2); !errors.Is(err, ErrConflict) {
		t.Fatal("content changed", err)
	}
	if _, err = repo.Complete(ctx, scope, second, object); err != nil {
		t.Fatal(err)
	}
	if _, err = repo.Complete(ctx, scope, second, object); err != nil {
		t.Fatal("completion replay failed", err)
	}
	third, err := repo.Reserve(ctx, scope, hash, 2)
	if err != nil || third.Ready == nil || third.Ready.ID != first.ArtifactID {
		t.Fatal("ready replay failed", err)
	}
	var stored, reserved int64
	if err = db.QueryRow(ctx, "SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1", tenant).Scan(&stored, &reserved); err != nil || stored != 2 || reserved != 0 {
		t.Fatal("quota charged twice", stored, reserved, err)
	}
	scope.Tenant = uuid.New()
	if _, err = repo.Reserve(ctx, scope, hash, 2); !errors.Is(err, ErrStale) {
		t.Fatal("cross tenant reservation accepted", err)
	}
}

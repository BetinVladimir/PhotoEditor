package review

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIntegrationDecisionIntegrity(t *testing.T) {
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
	var user, tenant, artifact, snapshot, run, review uuid.UUID
	var epoch int64
	if err = db.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", uuid.NewString()+"@example.test").Scan(&user, &epoch); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, "SELECT id FROM photo.tenants WHERE owner_user_id=$1", user).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	if err = db.QueryRow(ctx, "INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type) VALUES($1,'test',$2,$3,2,'application/json') RETURNING id", tenant, uuid.NewString(), hash).Scan(&artifact); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, "INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'input','user',$2,$3,1) RETURNING id", tenant, user, hash).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, "INSERT INTO photo.runs(tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES($1,'apply',$2,'{}',$3,$4) RETURNING id", tenant, snapshot, hash, user).Scan(&run); err != nil {
		t.Fatal(err)
	}
	if err = db.QueryRow(ctx, "INSERT INTO photo.reviews(tenant_id,run_id,candidate_artifact_id,candidate_hash,kind,reasons) VALUES($1,$2,$3,$4,'defects','[]') RETURNING id", tenant, run, artifact, hash).Scan(&review); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		"UPDATE photo.reviews SET candidate_hash=repeat('b',64) WHERE id=$1",
		"UPDATE photo.reviews SET revision=revision+1 WHERE id=$1",
		"UPDATE photo.reviews SET selection='{}' WHERE id=$1",
	} {
		if _, err = db.Exec(ctx, sql, review); err == nil {
			t.Fatalf("invalid pending mutation accepted: %s", sql)
		}
	}
	if _, err = db.Exec(ctx, "UPDATE photo.reviews SET state='decided',decision='reject',decided_by=$2,decided_at=now(),revision=2,decision_hash=$3,decision_route='base',selection='{}' WHERE id=$1", review, user, hash); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		"UPDATE photo.reviews SET decision='accept' WHERE id=$1",
		"UPDATE photo.reviews SET selection='[]' WHERE id=$1",
		"UPDATE photo.reviews SET decision_hash=repeat('b',64) WHERE id=$1",
		"UPDATE photo.reviews SET revision=3 WHERE id=$1",
	} {
		if _, err = db.Exec(ctx, sql, review); err == nil {
			t.Fatalf("decided mutation accepted: %s", sql)
		}
	}
	if _, err = db.Exec(ctx, "UPDATE photo.reviews SET revision=revision WHERE id=$1", review); err != nil {
		t.Fatal("no-op should be harmless", err)
	}
}

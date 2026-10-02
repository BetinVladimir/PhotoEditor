package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestIntegrationArtifactLeaseCannotAttachDeletingOrExpired(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated PostgreSQL with migration71 required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("database configuration failed")
	}
	defer db.Close()
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var user, tenant, artifact uuid.UUID
	var epoch int64
	if err = tx.QueryRow(ctx, "SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)", uuid.NewString()+"@example.test").Scan(&user, &epoch); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, "SELECT id FROM photo.tenants WHERE owner_user_id=$1", user).Scan(&tenant); err != nil {
		t.Fatal(err)
	}
	if err = tx.QueryRow(ctx, "INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type) VALUES($1,'test',$2,$3,1,'application/json') RETURNING id", tenant, uuid.NewString(), strings.Repeat("a", 64)).Scan(&artifact); err != nil {
		t.Fatal(err)
	}
	for _, state := range []string{"ready", "deleting", "expired"} {
		dbState := state
		if state == "expired" {
			dbState = "ready"
		}
		if _, err = tx.Exec(ctx, "UPDATE photo.artifacts SET state=$2,expires_at=CASE WHEN $3 THEN now()-interval '1 second' ELSE NULL END WHERE id=$1", artifact, dbState, state == "expired"); err != nil {
			t.Fatal(err)
		}
		save, err := tx.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, insertErr := save.Exec(ctx, "INSERT INTO photo.artifact_leases(tenant_id,artifact_id,holder_type,holder_id,expires_at) VALUES($1,$2,'test',$3,now()+interval '1 hour')", tenant, artifact, uuid.New())
		save.Rollback(ctx)
		if state == "ready" {
			if insertErr != nil {
				t.Fatal(insertErr)
			}
		} else {
			var pg *pgconn.PgError
			if !errors.As(insertErr, &pg) || pg.Code != "23514" {
				t.Fatal("unsafe artifact reference accepted", state, insertErr)
			}
		}
	}
	// A pre-existing snapshot must not bypass artifact state checks on a new run.
	if _, err = tx.Exec(ctx, "UPDATE photo.artifacts SET state='ready',expires_at=NULL WHERE id=$1", artifact); err != nil {
		t.Fatal(err)
	}
	var snapshot uuid.UUID
	if err = tx.QueryRow(ctx, "INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'input','user',$2,$3,1) RETURNING id", tenant, user, strings.Repeat("a", 64)).Scan(&snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,artifact_id,sha256,payload) VALUES($1,$2,0,$3,$4,'{}')", tenant, snapshot, artifact, strings.Repeat("a", 64)); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "UPDATE photo.artifacts SET state='deleting' WHERE id=$1", artifact); err != nil {
		t.Fatal(err)
	}
	save, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, attachErr := save.Exec(ctx, "INSERT INTO photo.runs(tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES($1,'apply',$2,'{}',$3,$4)", tenant, snapshot, strings.Repeat("a", 64), user)
	save.Rollback(ctx)
	var pg *pgconn.PgError
	if !errors.As(attachErr, &pg) || pg.Code != "23514" {
		t.Fatal("snapshot reused deleting source", attachErr)
	}

}

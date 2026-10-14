package semanticproposal

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Read-only isolation checks against the owned native worker fixture. This
// complements content tests; it does not establish science or GPU qualification.
func TestIntegrationSavedDefectConfigIsolation(t *testing.T) {
	raw := os.Getenv("PHOTO_SEMANTIC_INTENT_PUBLISH_CASE")
	if raw == "" || os.Getenv("PHOTO_TEST_V27_GATEWAY_TASK") == "" {
		t.Skip("owned native worker fixture required")
	}
	var fixture struct {
		Tenant, Project, Actor, Session uuid.UUID
		NativeForeign                   struct{ Tenant, Project, Actor, Session uuid.UUID }
	}
	if json.Unmarshal([]byte(raw), &fixture) != nil {
		t.Fatal("invalid owned fixture")
	}
	task, err := uuid.Parse(os.Getenv("PHOTO_TEST_V27_GATEWAY_TASK"))
	if err != nil {
		t.Fatal("invalid owned task")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg, err := pgxpool.ParseConfig(os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal("database configuration")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("database unavailable")
	}
	defer db.Close()
	var run uuid.UUID
	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", fixture.Tenant.String()); err == nil {
		err = tx.QueryRow(ctx, "SELECT run_id FROM photo.tasks WHERE tenant_id=$1 AND id=$2", fixture.Tenant, task).Scan(&run)
	}
	_ = tx.Rollback(ctx)
	if err != nil {
		t.Fatal("owned child run", err)
	}
	for _, name := range []string{"exact", "wrong_session", "wrong_actor", "wrong_project", "wrong_run", "foreign_tenant", "foreign_principal"} {
		t.Run(name, func(t *testing.T) {
			tid, pid, actor, session, rid := fixture.Tenant, fixture.Project, fixture.Actor, fixture.Session, run
			switch name {
			case "wrong_session":
				session = uuid.New()
			case "wrong_actor":
				actor = uuid.New()
			case "wrong_project":
				pid = uuid.New()
			case "wrong_run":
				rid = uuid.New()
			case "foreign_tenant":
				tid = fixture.NativeForeign.Tenant
				pid = fixture.NativeForeign.Project
				actor = fixture.NativeForeign.Actor
				session = fixture.NativeForeign.Session
			case "foreign_principal":
				actor = fixture.NativeForeign.Actor
				session = fixture.NativeForeign.Session
			}
			tx, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tid.String()); err != nil {
				t.Fatal(err)
			}
			var body []byte
			var sha string
			err = tx.QueryRow(ctx, "SELECT config_snapshot,config_hash FROM photo.semantic_defect_saved_config($1,$2,$3,$4,$5)", tid, pid, actor, session, rid).Scan(&body, &sha)
			if name == "exact" {
				if err != nil || len(body) < 2 || len(sha) != 64 {
					t.Fatal("owned config denied", err)
				}
			} else if err != pgx.ErrNoRows || len(body) != 0 || sha != "" {
				t.Fatal("foreign config returned", err)
			}
		})
	}
	for role, want := range map[string]bool{"photo_api": true, "photo_scheduler": false, "photo_worker": false} {
		var allowed bool
		if err := db.QueryRow(ctx, "SELECT has_function_privilege($1,'photo.semantic_defect_saved_config(uuid,uuid,uuid,uuid,uuid)','EXECUTE')", role).Scan(&allowed); err != nil || allowed != want {
			t.Fatal("saved config ACL", role, err)
		}
	}
	var publicAllowed bool
	if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_proc p,
LATERAL aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a
WHERE p.oid='photo.semantic_defect_saved_config(uuid,uuid,uuid,uuid,uuid)'::regprocedure
AND a.grantee=0 AND a.privilege_type='EXECUTE')`).Scan(&publicAllowed); err != nil || publicAllowed {
		t.Fatal("saved config PUBLIC ACL", err)
	}
	t.Log("V27_SAVED_DEFECT_CONFIG_ISOLATION_OK")
}

package v2draft

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/v2input"
)

func TestIdentityResultCatalogRejectsIncompleteScope(t *testing.T) {
	catalog := SQLIdentityResultCatalog{Bucket: "photos"}
	_, err := catalog.ResolveIdentityResultObject(context.Background(), IdentityResultScope{}, "report", v2input.Ref{})
	if err == nil {
		t.Fatal("empty scope accepted")
	}
}

// Requires an isolated database migrated through 02154. The definition and
// role matrix are checked without creating product data.
func TestIntegrationIdentityResultObjectRoleScope(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var def string
	var scheduler, api, worker bool
	err = pool.QueryRow(ctx, `SELECT pg_get_functiondef('photo.v23_identity_result_object(uuid,uuid,uuid,uuid,text,text,uuid,text,text)'::regprocedure),
	 has_function_privilege('photo_scheduler','photo.v23_identity_result_object(uuid,uuid,uuid,uuid,text,text,uuid,text,text)','EXECUTE'),
	 has_function_privilege('photo_api','photo.v23_identity_result_object(uuid,uuid,uuid,uuid,text,text,uuid,text,text)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.v23_identity_result_object(uuid,uuid,uuid,uuid,text,text,uuid,text,text)','EXECUTE')`).Scan(&def, &scheduler, &api, &worker)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"operation='identity.compare'", "canonical_result_id=p_result",
		"result_row.outputs IS DISTINCT FROM", "produced_fence=task_row.fencing_token",
		"photo.output_uploads", "u.state='ready'"} {
		if !strings.Contains(def, required) {
			t.Fatal("missing scoped gate", required)
		}
	}
	if !scheduler || api || worker {
		t.Fatal("identity locator role exposure")
	}
	// An arbitrary result has no authority, even under a permitted scheduler role.
	var raw []byte
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	config.ConnConfig.RuntimeParams["role"] = "photo_scheduler"
	schedulerPool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer schedulerPool.Close()
	tx, err := schedulerPool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.Background())
	tenant := uuid.New()
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		t.Fatal(err)
	}
	err = tx.QueryRow(ctx, `SELECT photo.v23_identity_result_object($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		tenant, uuid.New(), uuid.New(), uuid.New(), strings.Repeat("a", 64), "report", uuid.New(), strings.Repeat("b", 64), "photos").Scan(&raw)
	if err != nil {
		t.Fatal(err)
	}
	if raw != nil {
		t.Fatal("foreign result locator returned an object")
	}
}

func TestIntegrationIdentityResultInputRoleScope(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	var definition string
	var scheduler, api, worker bool
	err = pool.QueryRow(ctx, `SELECT pg_get_functiondef('photo.v23_identity_result_input(uuid,uuid,uuid,uuid,text,text,uuid,text,text)'::regprocedure),
	 has_function_privilege('photo_scheduler','photo.v23_identity_result_input(uuid,uuid,uuid,uuid,text,text,uuid,text,text)','EXECUTE'),
	 has_function_privilege('photo_api','photo.v23_identity_result_input(uuid,uuid,uuid,uuid,text,text,uuid,text,text)','EXECUTE'),
	 has_function_privilege('photo_worker','photo.v23_identity_result_input(uuid,uuid,uuid,uuid,text,text,uuid,text,text)','EXECUTE')`).Scan(&definition, &scheduler, &api, &worker)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"operation='identity.compare'", "canonical_result_id=p_result",
		"p_artifact IS DISTINCT FROM task_row.params_artifact_id", "i.contract=expected_contract",
		"i.project_id=project", "a.object_version NOT IN('','null')"} {
		if !strings.Contains(definition, required) {
			t.Fatal("input source gate missing", required)
		}
	}
	if !scheduler || api || worker {
		t.Fatal("input locator role exposure")
	}
}

package delivery

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
)

func TestIntegrationDeliveryListFunctionInstalled(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated database migrated through 01786 required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.WithoutCancel(ctx))
	var owner, definition, config string
	var secdef, api, worker, scheduler, pub bool
	err = conn.QueryRow(ctx, `SELECT pg_get_userbyid(p.proowner),p.prosecdef,pg_get_functiondef(p.oid),coalesce(array_to_string(p.proconfig,','),''),
 has_function_privilege('photo_api',p.oid,'EXECUTE'),has_function_privilege('photo_worker',p.oid,'EXECUTE'),
 has_function_privilege('photo_scheduler',p.oid,'EXECUTE'),EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE')
 FROM pg_proc p WHERE p.oid=to_regprocedure('photo.list_v30_delivery_packages(uuid,uuid,uuid,uuid,timestamptz,uuid,integer)')`).Scan(&owner, &secdef, &definition, &config, &api, &worker, &scheduler, &pub)
	if err != nil {
		t.Fatal(err)
	}
	if owner != "photo_fn_owner" || !secdef || !api || worker || scheduler || pub || !strings.Contains(strings.ReplaceAll(config, " ", ""), "search_path=pg_catalog,photo") {
		t.Fatal("delivery list authority drift")
	}
	for _, needle := range []string{"authorize_tenant(", "current_setting(", "project_acl", "order by d.created_at, d.id", "limit p_limit"} {
		if !strings.Contains(strings.Join(strings.Fields(strings.ToLower(definition)), ""), strings.Join(strings.Fields(needle), "")) {
			t.Fatal("delivery list definition drift", needle)
		}
	}
}

func TestIntegrationDeliveryListRefusesForeignTenantAndUnknownSession(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated database migrated through 01786 required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.WithoutCancel(ctx))
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	tenantID, otherID, actor, session, project := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_api"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenantID.String()); err != nil {
		t.Fatal(err)
	}
	rows, err := tx.Query(ctx, `SELECT id FROM photo.list_v30_delivery_packages($1,$2,$3,$4,NULL,NULL,10)`, tenantID, actor, session, project)
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		rows.Close()
		t.Fatal("unknown session saw delivery")
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	// A mismatched GUC is rejected before the function can inspect rows.
	if _, err = tx.Exec(ctx, "SAVEPOINT delivery_list_foreign"); err != nil {
		t.Fatal(err)
	}
	rows, err = tx.Query(ctx, `SELECT id FROM photo.list_v30_delivery_packages($1,$2,$3,$4,NULL,NULL,10)`, otherID, actor, session, project)
	if err == nil {
		for rows.Next() {
		}
		err = rows.Err()
		rows.Close()
	}
	if err == nil {
		t.Fatal("foreign tenant list accepted")
	}
	if _, rollbackErr := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT delivery_list_foreign"); rollbackErr != nil {
		t.Fatal(rollbackErr)
	}
}

func TestIntegrationDeliveryListOwnedPagesAndRestrictedACL(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated database migrated through 01786 required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.WithoutCancel(ctx))
	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	var owner, viewer, tenantID, ownedProject, privateProject, snapshot, policy uuid.UUID
	scan := func(query string, dest any, args ...any) {
		t.Helper()
		if e := tx.QueryRow(ctx, query, args...).Scan(dest); e != nil {
			t.Fatal(e)
		}
	}
	scan("SELECT user_id FROM photo.auth_bootstrap_user($1,'en',true)", &owner, uuid.NewString()+"@delivery-list.test")
	scan("SELECT user_id FROM photo.auth_bootstrap_user($1,'en',true)", &viewer, uuid.NewString()+"@delivery-list.test")
	scan("SELECT id FROM photo.tenants WHERE owner_user_id=$1", &tenantID, owner)
	ownerSession, viewerSession := uuid.New(), uuid.New()
	for _, pair := range []struct{ user, session uuid.UUID }{{owner, ownerSession}, {viewer, viewerSession}} {
		if _, err = tx.Exec(ctx, "INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')", pair.session, pair.user, auth.HashSecret(uuid.NewString()), uuid.New()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = tx.Exec(ctx, "INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'viewer','active')", tenantID, viewer); err != nil {
		t.Fatal(err)
	}
	scan("INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'Visible to ACL','restricted') RETURNING id", &ownedProject, tenantID)
	scan("INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'Hidden from viewer','restricted') RETURNING id", &privateProject, tenantID)
	if _, err = tx.Exec(ctx, "INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'viewer')", tenantID, ownedProject, viewer); err != nil {
		t.Fatal(err)
	}
	hash := strings.Repeat("a", 64)
	scan("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'v2','user',$2,$3,0) RETURNING id", &snapshot, tenantID, owner, hash)
	scan("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES($1,'test',$2,$3,2,'application/json','policy-v1',now()) RETURNING id", &policy, tenantID, uuid.NewString(), hash)
	insertPackage := func(project uuid.UUID, when time.Time) uuid.UUID {
		t.Helper()
		var run, workflow, packageID uuid.UUID
		scan("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) VALUES($1,'v2','running',$2,jsonb_build_object('project_id',$5::text),$3,$4,'fail_fast',true) RETURNING id", &run, tenantID, snapshot, hash, owner, project)
		scan("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES($1,$2,$3,$4,'delivery','v2.2',$5) RETURNING id", &workflow, tenantID, project, run, snapshot, hash)
		scan("INSERT INTO photo.delivery_packages(tenant_id,project_id,workflow_id,snapshot_id,name,policy_artifact_id,state,created_at) VALUES($1,$2,$3,$4,'Delivery',$5,'building',$6) RETURNING id", &packageID, tenantID, project, workflow, snapshot, policy, when)
		return packageID
	}
	firstAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	first := insertPackage(ownedProject, firstAt)
	second := insertPackage(ownedProject, firstAt.Add(time.Second))
	insertPackage(privateProject, firstAt)
	if _, err = tx.Exec(ctx, "SET LOCAL ROLE photo_api"); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenantID.String()); err != nil {
		t.Fatal(err)
	}
	read := func(actor, session, project uuid.UUID, afterAt *time.Time, afterID *uuid.UUID) ([]uuid.UUID, error) {
		rows, e := tx.Query(ctx, "SELECT id FROM photo.list_v30_delivery_packages($1,$2,$3,$4,$5,$6,10)", tenantID, actor, session, project, afterAt, afterID)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		ids := []uuid.UUID{}
		for rows.Next() {
			var id uuid.UUID
			if e = rows.Scan(&id); e != nil {
				return nil, e
			}
			ids = append(ids, id)
		}
		return ids, rows.Err()
	}
	ids, err := read(owner, ownerSession, ownedProject, nil, nil)
	if err != nil || len(ids) != 2 || ids[0] != first || ids[1] != second {
		t.Fatal("owner list order", ids, err)
	}
	ids, err = read(owner, ownerSession, ownedProject, &firstAt, &first)
	if err != nil || len(ids) != 1 || ids[0] != second {
		t.Fatal("keyset page", ids, err)
	}
	ids, err = read(viewer, viewerSession, ownedProject, nil, nil)
	if err != nil || len(ids) != 2 {
		t.Fatal("viewer ACL list", ids, err)
	}
	ids, err = read(viewer, viewerSession, privateProject, nil, nil)
	if err != nil || len(ids) != 0 {
		t.Fatal("restricted project leaked", ids, err)
	}
}

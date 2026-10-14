package delivery

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"testing"
)

// Requires an isolated already migrated database. Never applies migrations.
func TestCreateDocumentReservationRoleAndForeignScope(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, role := range []string{"photo_api", "photo_worker", "photo_scheduler", "photo_gateway", "photo_portal"} {
		t.Run(role, func(t *testing.T) {
			tx, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback(ctx)
			if _, err := tx.Exec(ctx, "SET LOCAL ROLE "+role); err != nil {
				t.Fatal(err)
			}
			var outcome string
			err = tx.QueryRow(ctx, `SELECT outcome FROM photo.reserve_v30_delivery_create_document($1,$2,$3,'key',$4,$5,'task_params','{}','documents')`, uuid.New(), uuid.New(), uuid.New(), digest([]byte("request")), uuid.New()).Scan(&outcome)
			if role == "photo_api" {
				if err != nil || outcome != "forbidden" {
					t.Fatal("foreign scope accepted", outcome, err)
				}
				return
			}
			var pgerr *pgconn.PgError
			if !errors.As(err, &pgerr) || pgerr.Code != "42501" {
				t.Fatal("runtime role must be denied", err)
			}
		})
	}
}

// Exercises the installed function's role boundary, without changing schema or
// creating production data. Positive session recovery needs the full fixture.
func TestPrepareProofRecoveryRoleAndForeignTenant(t *testing.T) {
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL")
 if dsn=="" {t.Skip("isolated migrated PostgreSQL required")}
 ctx:=context.Background()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil {t.Fatal(err)};defer db.Close()
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"} {
  t.Run(role,func(t *testing.T){
   tx,err:=db.Begin(ctx);if err!=nil {t.Fatal(err)};defer tx.Rollback(ctx)
   if _,err=tx.Exec(ctx,"SET LOCAL ROLE "+role);err!=nil {t.Fatal(err)}
   if _,err=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,uuid.New().String());err!=nil {t.Fatal(err)}
   var accepted bool
   err=tx.QueryRow(ctx,`SELECT photo.save_v30_delivery_prepare_proof($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb)`,uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New(),digest([]byte("{}")),"{}","{}").Scan(&accepted)
   if role=="photo_api" {if err!=nil||accepted {t.Fatal("foreign tenant accepted",err)};return}
   var denied *pgconn.PgError
   if !errors.As(err,&denied)||denied.Code!="42501" {t.Fatal("non-API role could invoke recovery",err)}
  })
 }
}

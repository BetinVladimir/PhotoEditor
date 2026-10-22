//go:build integration

package workflow

import (
 "context"
 "encoding/json"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

// The isolated fixture supplies a ready task_params document on an admitted,
// unsealed Lens preview draft with no task_inputs or other references. All
// transitions roll back; this never qualifies the fixture by migration text.
func TestLensPreviewDraftParamsGCTransitions(t *testing.T) {
 raw:=os.Getenv("PHOTO_TEST_LENS_DRAFT_GC")
 if raw=="" {t.Skip("isolated admitted Lens draft Params fixture required")}
 var f struct { Tenant, Run, Artifact uuid.UUID }
 if json.Unmarshal([]byte(raw),&f)!=nil || f.Tenant==uuid.Nil || f.Run==uuid.Nil || f.Artifact==uuid.Nil {t.Fatal("invalid fixture")}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,os.Getenv("PHOTO_TEST_DATABASE_URL"));if err!=nil {t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil {t.Fatal(err)};defer tx.Rollback(ctx)
 var exact bool
 err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT FROM photo.runs r JOIN photo.backend_documents d ON d.tenant_id=r.tenant_id AND d.run_id=r.id WHERE r.tenant_id=$1 AND r.id=$2 AND r.kind='preview' AND r.status='draft' AND NOT r.graph_sealed AND NOT r.cancel_requested AND r.v25_lens_preview_task_id IS NOT NULL AND d.artifact_id=$3 AND d.state='ready' AND d.purpose='task_params' AND d.v25_lens_preview_params IS NOT NULL)`,f.Tenant,f.Run,f.Artifact).Scan(&exact)
 if err!=nil || !exact {t.Fatal("fixture lacks exact live Lens draft",err)}
 assert:=func(want bool){t.Helper();var got bool;if err:=tx.QueryRow(ctx,"SELECT photo.v2_artifact_has_references($1,$2)",f.Tenant,f.Artifact).Scan(&got);err!=nil || got!=want {t.Fatalf("GC retained=%v want=%v err=%v",got,want,err)}}
 assert(true)
 if _,err=tx.Exec(ctx,"UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=$1 AND id=$2",f.Tenant,f.Run);err!=nil {t.Fatal(err)}
 assert(false)
}

// Cleanup evidence/token must already be provisioned by the normal isolated
// tenant cleaner fixture. Exercise the actual current purge, including its
// immediate constraint check, with Lens runs still pointing to their tasks.
func TestLensPreviewTenantPurgeDeferredProducerReference(t *testing.T) {
 raw:=os.Getenv("PHOTO_TEST_LENS_READY_PURGE")
 if raw=="" {t.Skip("isolated cleanup-confirmed Lens tenant fixture required")}
 var f struct {Tenant, Job, Token, Preview, Producer uuid.UUID}
 if json.Unmarshal([]byte(raw),&f)!=nil || f.Tenant==uuid.Nil || f.Job==uuid.Nil || f.Token==uuid.Nil || f.Preview==uuid.Nil || f.Producer==uuid.Nil {t.Fatal("invalid fixture")}
 ctx,cancel:=context.WithTimeout(context.Background(),15*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,os.Getenv("PHOTO_TEST_DATABASE_URL"));if err!=nil {t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil {t.Fatal(err)};defer tx.Rollback(ctx)
 var exact bool
 err=tx.QueryRow(ctx,`SELECT EXISTS(SELECT FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.id=r.v25_lens_preview_task_id WHERE r.tenant_id=$1 AND r.id=$2 AND t.id=$3 AND r.status NOT IN('draft','queued','running','waiting_review','cancelling'))`,f.Tenant,f.Preview,f.Producer).Scan(&exact)
 if err!=nil || !exact {t.Fatal("fixture lacks terminal Lens preview producer reference",err)}
 var finished bool
 if err=tx.QueryRow(ctx,"SELECT photo.finish_tenant_deletion($1,$2)",f.Job,f.Token).Scan(&finished);err!=nil || !finished {t.Fatal("cleanup-confirmed purge failed",finished,err)}
 var remaining int
 if err=tx.QueryRow(ctx,`SELECT (SELECT count(*) FROM photo.runs WHERE tenant_id=$1)+(SELECT count(*) FROM photo.tasks WHERE tenant_id=$1)+(SELECT count(*) FROM photo.v25_lens_profile_catalog WHERE tenant_id=$1)`,f.Tenant).Scan(&remaining);err!=nil || remaining!=0 {t.Fatal("Lens tenant metadata survived purge",remaining,err)}
 if _,err=tx.Exec(ctx,"SET CONSTRAINTS ALL IMMEDIATE");err!=nil {t.Fatal("Lens producer FK obstructed purge",err)}
}

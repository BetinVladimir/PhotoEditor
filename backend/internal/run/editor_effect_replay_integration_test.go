package run

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
)

// The fixture is a genuinely accepted Editor build with live original draft,
// source and actor/session authority. No origin predicate or stored receipt is
// mocked. This does not qualify rendering hardware or manufacture an acceptance.
func TestIntegrationEditorEffectsAcceptedReplayWithoutFreshDependencies(t *testing.T){
 dsn,raw:=os.Getenv("PHOTO_TEST_DATABASE_URL"),os.Getenv("PHOTO_TEST_EDITOR_ACCEPTED_FIXTURE")
 if dsn==""||raw==""{t.Skip("isolated migrated DB and actual accepted Editor fixture required")}
 var f struct{Tenant,Project,Actor,Session,ForeignActor,ForeignSession uuid.UUID;Key string;Request EditorEffectRequest}
 if json.Unmarshal([]byte(raw),&f)!=nil||f.Tenant==uuid.Nil||f.Project==uuid.Nil||f.Actor==uuid.Nil||f.Session==uuid.Nil||f.ForeignActor==uuid.Nil||f.ForeignSession==uuid.Nil||f.Key==""{t.Fatal("invalid fixture")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 cfg,e:=pgxpool.ParseConfig(dsn);if e!=nil{t.Fatal(e)};cfg.ConnConfig.RuntimeParams["role"]="photo_api"
 db,e:=pgxpool.NewWithConfig(ctx,cfg);if e!=nil{t.Fatal(e)};defer db.Close()
 who:=auth.Identity{UserID:f.Actor,SessionID:f.Session}
 // Drafts.DB/Objects absent and calibration explicitly withdrawn. Recovery
 // must authorize the real origin, not try to open the source or select workers.
 svc:=EditorEffectsService{Runs:Service{DB:db},FreshRenderInstalled:false}
 first,e:=svc.Begin(ctx,who,f.Tenant,f.Project,f.Key,f.Request);if e!=nil||!first.Replayed||first.ResourceID==nil||!validEditorEffectReceipt(first.Status,first.Body,*first.ResourceID){t.Fatal("accepted replay requires fresh dependencies",e)}
 again,e:=svc.Begin(ctx,who,f.Tenant,f.Project,f.Key,f.Request);if e!=nil||!again.Replayed||!bytes.Equal(first.Body,again.Body){t.Fatal("original receipt changed",e)}
 expect:=func(name string,actor auth.Identity,key string,req EditorEffectRequest,code string){t.Helper();t.Run(name,func(t *testing.T){_,err:=svc.Begin(ctx,actor,f.Tenant,f.Project,key,req);var d *domain.Error;if !errors.As(err,&d)||d.Code!=code{t.Fatalf("expected %s, got %v",code,err)}})}
 expect("fresh unavailable",who,uuid.NewString(),f.Request,"RENDER_UNAVAILABLE")
 altered:=f.Request;altered.DraftRevision++
 expect("changed immutable body",who,f.Key,altered,"IDEMPOTENCY_CONFLICT")
 // The foreign fixture actor must retain project Editor access; its request is
 // nevertheless fresh because receipts are scoped to the original principal.
 expect("foreign actor cannot replay",auth.Identity{UserID:f.ForeignActor,SessionID:f.ForeignSession},f.Key,f.Request,"RENDER_UNAVAILABLE")
}

package style

import (
 "context"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
)

// No worker or GPU qualification is fabricated. The unavailable advisory must
// still recheck project authority after its actual runtime-catalog wait.
func TestIntegrationAnalysisCapabilitiesWithdrawnDuringRuntimeLookup(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated fully migrated PostgreSQL required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 admin,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer admin.Close()
 cfg,err:=pgxpool.ParseConfig(dsn);if err!=nil{t.Fatal(err)}
 app:="style-analysis-authority-"+uuid.NewString();cfg.ConnConfig.RuntimeParams["role"]="photo_api";cfg.ConnConfig.RuntimeParams["application_name"]=app
 api,err:=pgxpool.NewWithConfig(ctx,cfg);if err!=nil{t.Fatal(err)};defer api.Close()
 exec:=func(query string,args ...any){t.Helper();if _,err:=admin.Exec(ctx,query,args...);err!=nil{t.Fatal(err)}}
 var owner,tid,projectID,styleID uuid.UUID;var who auth.Identity
 if err=admin.QueryRow(ctx,`SELECT user_id FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@analysis-owner.test").Scan(&owner);err!=nil{t.Fatal(err)}
 if err=admin.QueryRow(ctx,`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,owner).Scan(&tid);err!=nil{t.Fatal(err)}
 if err=admin.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@analysis-admin.test").Scan(&who.UserID,&who.Epoch);err!=nil{t.Fatal(err)}
 exec(`INSERT INTO photo.memberships(tenant_id,user_id,role) VALUES($1,$2,'admin')`,tid,who.UserID)
 if err=admin.QueryRow(ctx,`INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,'web',now()+interval '1 hour') RETURNING id`,who.UserID,auth.HashSecret(uuid.NewString()),uuid.New()).Scan(&who.SessionID);err!=nil{t.Fatal(err)}
 if err=admin.QueryRow(ctx,`INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,'Style private','restricted') RETURNING id`,tid).Scan(&projectID);err!=nil{t.Fatal(err)}
 if err=admin.QueryRow(ctx,`INSERT INTO photo.styles(tenant_id,project_id,name,created_by) VALUES($1,$2,'Style',$3) RETURNING id`,tid,projectID,who.UserID).Scan(&styleID);err!=nil{t.Fatal(err)}
 service:=Service{DB:api}
 positive,err:=service.AnalysisCapabilities(ctx,who,tid,styleID,true)
 if err!=nil||positive.Available||positive.Reason==nil{t.Fatal("unqualified positive advisory fixture invalid",positive,err)}
 blocker,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer blocker.Rollback(context.WithoutCancel(ctx))
 if _,err=blocker.Exec(ctx,`LOCK TABLE photo.worker_registrations IN ACCESS EXCLUSIVE MODE`);err!=nil{t.Fatal(err)}
 done:=make(chan error,1);go func(){_,err:=service.AnalysisCapabilities(ctx,who,tid,styleID,true);done<-err}()
 deadline:=time.Now().Add(3*time.Second)
 for {
  var waiting bool
  if err=admin.QueryRow(ctx,`SELECT EXISTS(SELECT FROM pg_stat_activity WHERE application_name=$1 AND cardinality(pg_blocking_pids(pid))>0 AND query LIKE '%select_style_analysis_runtimes%')`,app).Scan(&waiting);err!=nil{t.Fatal(err)}
  if waiting{break};select{case err:=<-done:t.Fatal("runtime wait not reached",err);default:}
  if time.Now().After(deadline){t.Fatal("runtime wait not observed")};time.Sleep(10*time.Millisecond)
 }
 exec(`UPDATE photo.memberships SET role='editor',revision=revision+1 WHERE tenant_id=$1 AND user_id=$2`,tid,who.UserID)
 if err=blocker.Commit(ctx);err!=nil{t.Fatal(err)}
 if err=<-done;err==nil||domain.AsError(err).Code!="NOT_FOUND"{t.Fatal("withdrawn Admin exposed project advisory",err)}
}

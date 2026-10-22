package v2render

import (
 "context"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/auth"
)

// Invoked by the isolated physical-report fixture after trusted qualification
// installation. The real catalog lock pauses lookup after its entry ACL read.
func assertRenderQualificationWithdrawnDuringLookup(t *testing.T,ctx context.Context,admin,api *pgxpool.Pool,e renderBatchRestartEvidence){
 t.Helper()
 var visibility string
 if err:=admin.QueryRow(ctx,`SELECT visibility FROM photo.projects WHERE tenant_id=$1 AND id=$2`,e.Tenant,e.Project).Scan(&visibility);err!=nil{t.Fatal(err)}
 // The runtime evidence often belongs to the tenant Owner. Never demote that
 // principal merely to create a restricted-project negative case.
 var actor uuid.UUID;var epoch int64
 if err:=admin.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@render-authority.test").Scan(&actor,&epoch);err!=nil{t.Fatal(err)}
 session:=uuid.New()
 if _,err:=admin.Exec(ctx,`INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`,session,actor,auth.HashSecret(uuid.NewString()),uuid.New());err!=nil{t.Fatal(err)}
 if _,err:=admin.Exec(ctx,`INSERT INTO photo.memberships(tenant_id,user_id,role) VALUES($1,$2,'editor')`,e.Tenant,actor);err!=nil{t.Fatal(err)}
 e.Actor=auth.Identity{UserID:actor,SessionID:session,Epoch:epoch}
 defer func(){
  restore,cancel:=context.WithTimeout(context.Background(),3*time.Second);defer cancel()
  _,_=admin.Exec(restore,`UPDATE photo.projects SET visibility=$3 WHERE tenant_id=$1 AND id=$2`,e.Tenant,e.Project,visibility)
  _,_=admin.Exec(restore,`DELETE FROM photo.project_acl WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3`,e.Tenant,e.Project,actor)
  _,_=admin.Exec(restore,`DELETE FROM photo.memberships WHERE tenant_id=$1 AND user_id=$2`,e.Tenant,actor)
 }()
 if _,err:=admin.Exec(ctx,`UPDATE photo.projects SET visibility='restricted' WHERE tenant_id=$1 AND id=$2`,e.Tenant,e.Project);err!=nil{t.Fatal(err)}
 if _,err:=admin.Exec(ctx,`INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'editor')`,e.Tenant,e.Project,actor);err!=nil{t.Fatal(err)}
 cfg:=api.Config().Copy();name:="render-authority-"+uuid.NewString();cfg.ConnConfig.RuntimeParams["application_name"]=name
 scoped,err:=pgxpool.NewWithConfig(ctx,cfg);if err!=nil{t.Fatal(err)};defer scoped.Close()
 if _,err=(SQLRenderQualificationResolver{API:scoped}).Resolve(ctx,e.Actor,e.Tenant,e.Project);err!=nil{t.Fatal("fixture not qualified before withdrawal",err)}
 lock,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer lock.Rollback(context.Background())
 if _,err=lock.Exec(ctx,`LOCK TABLE photo.v2_qualification_policies IN ACCESS EXCLUSIVE MODE`);err!=nil{t.Fatal(err)}
 work,cancel:=context.WithTimeout(ctx,5*time.Second);defer cancel();done:=make(chan error,1)
 go func(){_,err:=(SQLRenderQualificationResolver{API:scoped}).Resolve(work,e.Actor,e.Tenant,e.Project);done<-err}()
 deadline:=time.NewTimer(3*time.Second);defer deadline.Stop();tick:=time.NewTicker(10*time.Millisecond);defer tick.Stop()
 waiting:=false
 for !waiting{select{case <-deadline.C:t.Fatal("qualification lookup did not reach the held catalog lock");case err:=<-done:t.Fatal("lookup returned before lock",err);case <-tick.C:
  if err=admin.QueryRow(ctx,`SELECT EXISTS(SELECT FROM pg_stat_activity WHERE application_name=$1 AND wait_event_type='Lock' AND query LIKE '%resolve_v26_render_qualification%')`,name).Scan(&waiting);err!=nil{t.Fatal(err)}
 }}
 if _,err=admin.Exec(ctx,`UPDATE photo.project_acl SET role='viewer' WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3`,e.Tenant,e.Project,e.Actor.UserID);err!=nil{t.Fatal(err)}
 if err=lock.Commit(ctx);err!=nil{t.Fatal(err)}
 select{case err=<-done:if err==nil||domain.AsError(err).Code!="FORBIDDEN"{t.Fatal("withdrawn project authority exposed qualification",err)};case <-work.Done():t.Fatal("lookup did not finish",work.Err())}
}

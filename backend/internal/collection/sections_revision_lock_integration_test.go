package collection

import (
 "context"
 "errors"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
)

// Authored only: exercise the actual API-role reader against an uncommitted
// catalogue mutation, rather than mocking its two READ COMMITTED statements.
func TestIntegrationSectionsReadRevisionAndAuthorityAfterCollectionWait(t *testing.T){
 for _,withdraw:=range []bool{false,true}{t.Run(map[bool]string{false:"committed_page",true:"project_withdrawn"}[withdraw],func(t *testing.T){
  ctx,admin,service,who,tenant,collection,_:=sectionAbandonFixture(t)
  work,cancel:=context.WithTimeout(ctx,10*time.Second);defer cancel()
  blocker,err:=admin.Begin(work);if err!=nil{t.Fatal(err)};defer blocker.Rollback(context.Background())
  var pid int;if err=blocker.QueryRow(work,`SELECT pg_backend_pid()`).Scan(&pid);err!=nil{t.Fatal(err)}
  if _,err=blocker.Exec(work,`UPDATE photo.collections SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`,tenant,collection);err!=nil{t.Fatal(err)}
  if _,err=blocker.Exec(work,`INSERT INTO photo.sections(tenant_id,collection_id,name,position) VALUES($1,$2,'Committed section',0)`,tenant,collection);err!=nil{t.Fatal(err)}
  type outcome struct{revision string;count int;name string;err error};done:=make(chan outcome,1)
  go func(){page,e:=service.ListSections(work,who,tenant,collection,10,"");v:=outcome{revision:page.Revision,count:len(page.Items),err:e};if len(page.Items)>0{v.name=page.Items[0].Name};done<-v}()
  ticker:=time.NewTicker(10*time.Millisecond);defer ticker.Stop()
  for{var waiting bool;if err=admin.QueryRow(work,`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`,pid).Scan(&waiting);err!=nil{t.Fatal(err)};if waiting{break};select{case got:=<-done:t.Fatalf("section page escaped revision lock: %+v",got);case <-ticker.C:case <-work.Done():t.Fatal(work.Err())}}
  // A project can become inaccessible while the collection lock is awaited.
  if withdraw{if _,err=blocker.Exec(work,`UPDATE photo.projects SET archived_at=clock_timestamp() WHERE tenant_id=$1 AND id=(SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2)`,tenant,collection);err!=nil{t.Fatal(err)}}
  if err=blocker.Commit(work);err!=nil{t.Fatal(err)}
  select{case got:=<-done:
   if withdraw{if got.err==nil||got.count!=0{t.Fatalf("withdrew project exposed sections: %+v",got)}}else if got.err!=nil||got.revision!="2"||got.count!=1||got.name!="Committed section"{t.Fatalf("incoherent committed page: %+v",got)}
  case <-work.Done():t.Fatal(work.Err())}
 })}
}

func TestIntegrationCatalogueSharedReadersReauthorizeDemotedAdminAfterWait(t *testing.T){
 for _,reader:=range []string{"sections","items"}{t.Run(reader,func(t *testing.T){
  ctx,admin,service,_,tenant,collection,_:=sectionAbandonFixture(t)
  work,cancel:=context.WithTimeout(ctx,10*time.Second);defer cancel()
  var actor,session uuid.UUID;var epoch int64
  if err:=admin.QueryRow(work,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@test.example").Scan(&actor,&epoch);err!=nil{t.Fatal(err)}
  if err:=admin.QueryRow(work,`INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,'web',now()+interval '1 day') RETURNING id`,actor,auth.HashSecret(uuid.NewString()),uuid.New()).Scan(&session);err!=nil{t.Fatal(err)}
  if _,err:=admin.Exec(work,`INSERT INTO photo.memberships(tenant_id,user_id,role) VALUES($1,$2,'admin')`,tenant,actor);err!=nil{t.Fatal(err)}
  if _,err:=admin.Exec(work,`UPDATE photo.projects SET visibility='restricted' WHERE tenant_id=$1 AND id=(SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2)`,tenant,collection);err!=nil{t.Fatal(err)}
  who:=auth.Identity{UserID:actor,SessionID:session,Epoch:epoch}
  blocker,err:=admin.Begin(work);if err!=nil{t.Fatal(err)};defer blocker.Rollback(context.Background())
  var pid int;if err=blocker.QueryRow(work,`SELECT pg_backend_pid()`).Scan(&pid);err!=nil{t.Fatal(err)}
  if _,err=blocker.Exec(work,`UPDATE photo.collections SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`,tenant,collection);err!=nil{t.Fatal(err)}
  done:=make(chan error,1);go func(){if reader=="sections"{_,e:=service.ListSections(work,who,tenant,collection,10,"");done<-e}else{_,e:=service.ListItems(work,who,tenant,collection,10,"","","","","");done<-e}}()
  ticker:=time.NewTicker(10*time.Millisecond);defer ticker.Stop()
  for{var waiting bool;if err=admin.QueryRow(work,`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`,pid).Scan(&waiting);err!=nil{t.Fatal(err)};if waiting{break};select{case e:=<-done:t.Fatalf("reader did not await shared lock: %v",e);case <-ticker.C:case <-work.Done():t.Fatal(work.Err())}}
  if _,err=blocker.Exec(work,`UPDATE photo.memberships SET role='viewer',revision=revision+1 WHERE tenant_id=$1 AND user_id=$2`,tenant,actor);err!=nil{t.Fatal(err)}
  if err=blocker.Commit(work);err!=nil{t.Fatal(err)}
  select{case e:=<-done:var refusal *domain.Error;if !errors.As(e,&refusal)||refusal.Code!="NOT_FOUND"{t.Fatalf("demoted admin must lose restricted project access: %v",e)};case <-work.Done():t.Fatal(work.Err())}
 })}
}

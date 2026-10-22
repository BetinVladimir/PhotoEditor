package semanticproposal

import (
 "context"
 "encoding/json"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// This fixture must come from genuine ordinary semantic HTTP admission in an
// isolated migrated database. No sealed graph/qualification/receipt is forged
// by the test. Objects, publishers and producer configuration are deliberately
// absent from both tested services: a recovered 202 must not touch them.
func TestIntegrationOrdinaryCreateReceiptPrecedesProducer(t *testing.T){
 dsn,raw:=os.Getenv("PHOTO_TEST_DATABASE_URL"),os.Getenv("PHOTO_TEST_SEMANTIC_CREATE_RECEIPT")
 if dsn==""||raw==""{t.Skip("isolated DB and genuine committed semantic admission fixture required")}
 type attempt struct{Tenant,Actor,Session uuid.UUID;Key string;Intent v2input.SemanticEditIntent}
 var f struct{Accepted attempt;NewSession uuid.UUID;Pending attempt;Denied []attempt}
 if json.Unmarshal([]byte(raw),&f)!=nil||f.Accepted.Tenant==uuid.Nil||f.Accepted.Actor==uuid.Nil||f.Accepted.Session==uuid.Nil||f.Accepted.Key==""||f.NewSession==uuid.Nil||f.Pending.Key==""||len(f.Denied)<1{t.Fatal("fixture requires accepted/new-login/pending and revoked-role or project-denied identities")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 who:=auth.Identity{UserID:f.Accepted.Actor,SessionID:f.Accepted.Session}
 source:=CreateService{Preparer:TransferDraftPreparer{Reader:SQLTransferSourceReader{DB:db}}}
 projects:=&ProjectCreateService{api:db}
 first,err:=source.Propose(ctx,who,f.Accepted.Tenant,f.Accepted.Key,f.Accepted.Intent);if err!=nil||first.Status!=202||first.ResourceID==nil{t.Fatal("real accepted receipt did not recover without producers",first,err)}
 for _,identity:=range []auth.Identity{who,{UserID:who.UserID,SessionID:f.NewSession}}{
  got,err:=projects.Propose(ctx,identity,f.Accepted.Tenant,f.Accepted.Key,f.Accepted.Intent)
  if err!=nil||got.Status!=202||string(got.Body)!=string(first.Body)||got.ResourceID==nil||*got.ResourceID!=*first.ResourceID{t.Fatal("exact replay/new-login recovery changed receipt",got,err)}
 }
 changed:=f.Accepted.Intent;if changed.Strength==0{changed.Strength=1}else{changed.Strength=0}
 if got,err:=projects.Propose(ctx,who,f.Accepted.Tenant,f.Accepted.Key,changed);err==nil||got.Status!=0{t.Fatal("changed body recovered original",got,err)}
 for _,denied:=range f.Denied{if got,err:=projects.Propose(ctx,auth.Identity{UserID:denied.Actor,SessionID:denied.Session},denied.Tenant,denied.Key,denied.Intent);err==nil||got.Status!=0{t.Fatal("withdrawn tenant/project authority recovered ticket",got,err)}}
 for _,scope:=range []attempt{{Tenant:uuid.New(),Actor:who.UserID,Session:who.SessionID,Key:f.Accepted.Key,Intent:f.Accepted.Intent},{Tenant:f.Accepted.Tenant,Actor:uuid.New(),Session:who.SessionID,Key:f.Accepted.Key,Intent:f.Accepted.Intent}}{
  if got,_:=projects.Propose(ctx,auth.Identity{UserID:scope.Actor,SessionID:scope.Session},scope.Tenant,scope.Key,scope.Intent);got.Status!=0{t.Fatal("foreign scope recovered receipt")}
 }
 for _,pending:=range []attempt{f.Pending,{Tenant:f.Accepted.Tenant,Actor:who.UserID,Session:who.SessionID,Key:uuid.NewString(),Intent:f.Accepted.Intent}}{
  raw,e:=canonical.Marshal(pending.Intent);if e!=nil{t.Fatal(e)};compiled,e:=CompileRequest(pending.Tenant,pending.Actor,pending.Key,raw);if e!=nil{t.Fatal(e)}
  if got,found,e:=replayCreateReceipt(ctx,db,auth.Identity{UserID:pending.Actor,SessionID:pending.Session},compiled,pending.Key);e!=nil||found||got.Status!=0{t.Fatal("pending shell failed to fall through before final frozen-node validation",got,found,e)}
  // No configured producer means pending/missing admission cannot manufacture
  // a queued response from a deterministic run identity or partial shell.
  if got,err:=projects.Propose(ctx,auth.Identity{UserID:pending.Actor,SessionID:pending.Session},pending.Tenant,pending.Key,pending.Intent);err==nil||got.Status!=0{t.Fatal("missing/incomplete receipt became success",got,err)}
 }
}

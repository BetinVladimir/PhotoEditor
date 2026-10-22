package collection

import (
 "strings"
 "encoding/json"
 "photoeditor/backend/internal/canonical"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
)

// Authored API-role transactions with actual collection/share row locks. No
// tests executed; a disposable fully migrated database is required.
func TestIntegrationShareMutationRechecksProjectAfterShareWait(t *testing.T){
 for _,action:=range []string{"update","revoke"}{t.Run(action,func(t *testing.T){
  ctx,admin,service,owner,tid,parent,_:=sectionAbandonFixture(t);service.Pepper=[]byte(strings.Repeat("p",32));req:=shareIntentRequest()
  first,err:=service.CreateShare(ctx,owner,tid,parent,uuid.NewString(),req);if err!=nil{t.Fatal(err)}
  var project,user uuid.UUID;var epoch int64
  if err=admin.QueryRow(ctx,`SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2`,tid,parent).Scan(&project);err!=nil{t.Fatal(err)}
  if err=admin.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@share-authority.test").Scan(&user,&epoch);err!=nil{t.Fatal(err)}
  if _,err=admin.Exec(ctx,`INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'editor','active')`,tid,user);err!=nil{t.Fatal(err)}
  if _,err=admin.Exec(ctx,`UPDATE photo.projects SET visibility='restricted' WHERE tenant_id=$1 AND id=$2`,tid,project);err!=nil{t.Fatal(err)}
  if _,err=admin.Exec(ctx,`INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'editor')`,tid,project,user);err!=nil{t.Fatal(err)}
  session:=uuid.New();if _,err=admin.Exec(ctx,`INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`,session,user,auth.HashSecret(uuid.NewString()),uuid.New());err!=nil{t.Fatal(err)}
  who:=auth.Identity{UserID:user,SessionID:session,Epoch:epoch}
  holder,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer holder.Rollback(ctx)
  var blocker int;if err=holder.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&blocker);err!=nil{t.Fatal(err)}
  var held uuid.UUID;if err=holder.QueryRow(ctx,`SELECT id FROM photo.share_links WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,tid,first.ID).Scan(&held);err!=nil{t.Fatal(err)}
  label:="Changed";done:=make(chan error,1);go func(){var e error;if action=="update"{_,e=service.UpdateShare(ctx,who,tid,parent,first.ID,1,SharePatch{Label:&label})}else{_,e=service.RevokeShare(ctx,who,tid,parent,first.ID)};done<-e}()
  timer:=time.NewTimer(4*time.Second);defer timer.Stop();tick:=time.NewTicker(10*time.Millisecond);defer tick.Stop();blocked:=false
  for !blocked{select{case e:=<-done:t.Fatal("mutation did not wait",e);case <-timer.C:t.Fatal("share lock wait not observed");case <-tick.C:if err=admin.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`,blocker).Scan(&blocked);err!=nil{t.Fatal(err)}}}
  if _,err=admin.Exec(ctx,`DELETE FROM photo.project_acl WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3`,tid,project,user);err!=nil{t.Fatal(err)}
  if err=holder.Commit(ctx);err!=nil{t.Fatal(err)}
  select{case err=<-done:case <-ctx.Done():t.Fatal(ctx.Err())};if domain.AsError(err).Code!="NOT_FOUND"{t.Fatalf("withdrawal: %v",err)}
  var actualLabel string;var revision int64;var revoked bool
  if err=admin.QueryRow(ctx,`SELECT label,policy_version,revoked_at IS NOT NULL FROM photo.share_links WHERE tenant_id=$1 AND id=$2`,tid,first.ID).Scan(&actualLabel,&revision,&revoked);err!=nil||actualLabel!=first.Label||revision!=1||revoked{t.Fatalf("refusal side effects %s %d %v %v",actualLabel,revision,revoked,err)}
  var result Share;if action=="update"{result,err=service.UpdateShare(ctx,owner,tid,parent,first.ID,1,SharePatch{Label:&label})}else{result,err=service.RevokeShare(ctx,owner,tid,parent,first.ID)}
  if err!=nil||result.PolicyVersion!="2"||result.ID!=first.ID{t.Fatalf("authorized mutation: %+v %v",result,err)}
 })}
}


func TestIntegrationShareCreateReplayValidatesActualStoredReceipt(t *testing.T){
 ctx,admin,service,who,tid,parent,_:=sectionAbandonFixture(t);service.Pepper=[]byte(strings.Repeat("p",32));req:=shareIntentRequest()
 first,err:=service.CreateShare(ctx,who,tid,parent,uuid.NewString(),req);if err!=nil{t.Fatal(err)}
 intent,err:=freezeShareIntent(parent,req);if err!=nil{t.Fatal(err)};hash,err:=canonical.Hash(intent);if err!=nil{t.Fatal(err)}
 original:=ShareCreated{Share:first.Share};body,err:=canonical.Marshal(original);if err!=nil{t.Fatal(err)}
 service.Pepper=nil // Exact accepted replay must not require new-secret machinery.
 for _,scenario:=range []string{"valid","wrong_status","wrong_resource","foreign_parent","altered_policy","unknown_field"}{t.Run(scenario,func(t *testing.T){
  var fields map[string]any;if err=json.Unmarshal(body,&fields);err!=nil{t.Fatal(err)}
  status:=201;resource:=first.ID
  switch scenario{case "wrong_status":status=202;case "wrong_resource":resource=uuid.New();case "foreign_parent":fields["collection_id"]=uuid.NewString();case "altered_policy":fields["label"]="Altered";case "unknown_field":fields["secret_hash"]="must not escape"}
  stored,err:=canonical.Marshal(fields);if err!=nil{t.Fatal(err)};key:=uuid.NewString()
  if _,err=admin.Exec(ctx,`INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at,response_status,response_body,resource_id) VALUES($1,'user',$2,'Share.Create',$3,$4,now()+interval '1 day',$5,$6,$7)`,tid,who.UserID,key,hash,status,stored,resource);err!=nil{t.Fatal(err)}
  replay,err:=service.CreateShare(ctx,who,tid,parent,key,req)
  if scenario=="valid"{if err!=nil||replay.ID!=first.ID||replay.Secret!=""||!replay.Replayed{t.Fatalf("valid immutable replay: %+v %v",replay,err)}}else if domain.AsError(err).Code!="IDEMPOTENCY_CONFLICT"{t.Fatalf("malformed receipt escaped: %+v %v",replay,err)}
 })}
}


func TestIntegrationShareExplicitNanosecondIntentUsesExactPersistedMicroseconds(t *testing.T){
 ctx,_,service,who,tid,parent,_:=sectionAbandonFixture(t);service.Pepper=[]byte(strings.Repeat("p",32));req:=shareIntentRequest();expiry:=time.Now().UTC().Add(time.Hour).Truncate(time.Second).Add(123456789*time.Nanosecond);req.ExpiresAt=OptionalTime{Set:true,Value:&expiry};key:=uuid.NewString()
 first,err:=service.CreateShare(ctx,who,tid,parent,key,req);if err!=nil||first.ExpiresAt==nil||!first.ExpiresAt.Equal(expiry.Truncate(time.Microsecond)){t.Fatalf("fresh projection: %+v %v",first,err)}
 replay,err:=service.CreateShare(ctx,who,tid,parent,key,req);if err!=nil||replay.ID!=first.ID||!replay.Replayed||replay.Secret!=""{t.Fatalf("exact replay: %+v %v",replay,err)}
 accepted,err:=service.AbandonShareCreation(ctx,who,tid,parent,key,req);if err!=nil||accepted.Status!=201{t.Fatalf("accepted abandon: %+v %v",accepted,err)}
 changed:=req;different:=expiry.Add(time.Nanosecond);changed.ExpiresAt.Value=&different;if _,err=service.CreateShare(ctx,who,tid,parent,key,changed);domain.AsError(err).Code!="IDEMPOTENCY_CONFLICT"{t.Fatalf("original precise command was changed: %v",err)}
}

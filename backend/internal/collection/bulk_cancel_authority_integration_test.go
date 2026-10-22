package collection

import (
 "encoding/json"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
)

// Real API-role cancel against a committed selection/admission, while a second
// transaction holds the run lock. Authored only, not executed.
func TestIntegrationBulkCancelRechecksProjectAfterRunWait(t *testing.T){
 ctx,admin,service,who,tid,id,_:=sectionAbandonFixture(t)
 digest:="e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
 var project,artifact,asset,version,item uuid.UUID
 if err:=admin.QueryRow(ctx,`SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2`,tid,id).Scan(&project);err!=nil{t.Fatal(err)}
 if err:=admin.QueryRow(ctx,`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role) VALUES($1,'photo',$2,'v1',$3,0,'image/jpeg','working_image') RETURNING id`,tid,uuid.NewString(),digest).Scan(&artifact);err!=nil{t.Fatal(err)}
 if err:=admin.QueryRow(ctx,`INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`,tid,project,artifact).Scan(&asset);err!=nil{t.Fatal(err)}
 if err:=admin.QueryRow(ctx,`INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,'{}','accepted') RETURNING id`,tid,asset,artifact,digest).Scan(&version);err!=nil{t.Fatal(err)}
 if err:=admin.QueryRow(ctx,`INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,position) VALUES($1,$2,$3,$4,0) RETURNING id`,tid,id,asset,version).Scan(&item);err!=nil{t.Fatal(err)}
 var revision string;if err:=admin.QueryRow(ctx,`SELECT revision::text FROM photo.collections WHERE tenant_id=$1 AND id=$2`,tid,id).Scan(&revision);err!=nil{t.Fatal(err)}
 selection:=SelectionRequest{CollectionRevision:revision,Scope:"selected",ItemIDs:[]uuid.UUID{item}};selectionKey,key:=uuid.NewString(),uuid.NewString()
 selected,err:=service.CreateSelection(ctx,who,tid,id,selectionKey,selection);if err!=nil{t.Fatal(err)}
 var ref SnapshotRef;if err=json.Unmarshal(selected.Body,&ref);err!=nil{t.Fatal(err)}
 _,number,err:=normalizeSelection(selection);if err!=nil{t.Fatal(err)}
 req:=BulkRequest{ExpectedRevision:number,SelectionSnapshotID:ref.ID,Action:"rating",Params:[]byte(`{"rating":5}`)}
 accepted,err:=service.StartBulk(ctx,who,tid,id,key,req);if err!=nil{t.Fatal(err)}

 var ticket BulkAccepted;if err=json.Unmarshal(accepted.Body,&ticket);err!=nil{t.Fatal(err)}
 var user uuid.UUID;var epoch int64
 if err=admin.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@bulk-cancel.test").Scan(&user,&epoch);err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'editor','active')`,tid,user);err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`UPDATE photo.projects SET visibility='restricted' WHERE tenant_id=$1 AND id=$2`,tid,project);err!=nil{t.Fatal(err)}
 if _,err=admin.Exec(ctx,`INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'editor')`,tid,project,user);err!=nil{t.Fatal(err)}
 session:=uuid.New();if _,err=admin.Exec(ctx,`INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`,session,user,auth.HashSecret(uuid.NewString()),uuid.New());err!=nil{t.Fatal(err)}
 editor:=auth.Identity{UserID:user,SessionID:session,Epoch:epoch}
 holder,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer holder.Rollback(ctx)
 var blocker int;if err=holder.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&blocker);err!=nil{t.Fatal(err)}
 var held uuid.UUID;if err=holder.QueryRow(ctx,`SELECT id FROM photo.runs WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,tid,ticket.RunID).Scan(&held);err!=nil{t.Fatal(err)}
 done:=make(chan error,1);go func(){_,e:=service.CancelBulk(ctx,editor,tid,ticket.BulkOperationID);done<-e}()
 timer:=time.NewTimer(4*time.Second);defer timer.Stop();tick:=time.NewTicker(10*time.Millisecond);defer tick.Stop();blocked:=false
 for !blocked{select{case e:=<-done:t.Fatal("cancel did not wait",e);case <-timer.C:t.Fatal("run lock wait not observed");case <-tick.C:if err=admin.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`,blocker).Scan(&blocked);err!=nil{t.Fatal(err)}}}
 if _,err=admin.Exec(ctx,`DELETE FROM photo.project_acl WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3`,tid,project,user);err!=nil{t.Fatal(err)}
 if err=holder.Commit(ctx);err!=nil{t.Fatal(err)}
 select{case err=<-done:case <-ctx.Done():t.Fatal(ctx.Err())};if domain.AsError(err).Code!="NOT_FOUND"{t.Fatalf("withdrawal: %v",err)}
 var status string;var cancelled bool;if err=admin.QueryRow(ctx,`SELECT status,cancel_requested FROM photo.bulk_operations WHERE tenant_id=$1 AND id=$2`,tid,ticket.BulkOperationID).Scan(&status,&cancelled);err!=nil||status!="queued"||cancelled{t.Fatalf("cancel side effects: %s %v %v",status,cancelled,err)}
 // The still-authorized owner can cancel the same operation, obtaining the
 // actual full status DTO; repeating it is a read, not a second admission.
 result,err:=service.CancelBulk(ctx,who,tid,ticket.BulkOperationID);if err!=nil||result.Status!="cancelled"||!result.CancelRequested||result.ID!=ticket.BulkOperationID||result.Results.Items==nil{t.Fatalf("owner cancellation: %+v %v",result,err)}
 again,err:=service.CancelBulk(ctx,who,tid,ticket.BulkOperationID);if err!=nil||again.Status!="cancelled"{t.Fatalf("repeat cancel: %+v %v",again,err)}
}

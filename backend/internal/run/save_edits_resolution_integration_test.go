package run

import (
 "context"
 "encoding/json"
 "os"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
)
// This real API-role protocol fixture seeds the persisted accepted run/snapshot
// receipt, not physical rendering. Fresh producer quality is a separate boundary.
func TestIntegrationSaveEditsResolveReceiptOrders(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated PostgreSQL required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 admin,e:=pgxpool.New(ctx,dsn);if e!=nil{t.Fatal(e)};defer admin.Close();cfg,e:=pgxpool.ParseConfig(dsn);if e!=nil{t.Fatal(e)};cfg.ConnConfig.RuntimeParams["role"]="photo_api";api,e:=pgxpool.NewWithConfig(ctx,cfg);if e!=nil{t.Fatal(e)};defer api.Close()
 var actor,tid uuid.UUID;var epoch int64
 if e=admin.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@test.example").Scan(&actor,&epoch);e!=nil{t.Fatal(e)}
 if e=admin.QueryRow(ctx,`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,actor).Scan(&tid);e!=nil{t.Fatal(e)}
 session:=uuid.New();if _,e=admin.Exec(ctx,`INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`,session,actor,auth.HashSecret(uuid.NewString()),uuid.New());e!=nil{t.Fatal(e)}
 var projectID,collection,other,output uuid.UUID
 if e=admin.QueryRow(ctx,`INSERT INTO photo.projects(tenant_id,name) VALUES($1,'Save edits protocol') RETURNING id`,tid).Scan(&projectID);e!=nil{t.Fatal(e)}
 createCollection:=func(name string)uuid.UUID{var id uuid.UUID;if e:=admin.QueryRow(ctx,`INSERT INTO photo.collections(tenant_id,project_id,name) VALUES($1,$2,$3) RETURNING id`,tid,projectID,name).Scan(&id);e!=nil{t.Fatal(e)};return id}
 collection,other,output=createCollection("Source"),createCollection("Other"),createCollection("Output")
 who:=auth.Identity{UserID:actor,SessionID:session,Epoch:epoch};svc:=Service{DB:api};req:=SaveEditsRequest{SourceRevision:1,Scope:"selected",SelectedItemIDs:[]uuid.UUID{uuid.New()},OverridesID:uuid.New(),OutputName:"Edited"}
 key:=uuid.NewString();first,e:=svc.ResolveSaveEdits(ctx,who,tid,collection,key,req);if e!=nil||!isSaveEditsAbandoned(first,collection,key){t.Fatal("abandon fresh without mutable override/source checks",e)}
 // Collection may advance after an unknown command. Resolution still addresses
 // the old command, while normal late admission must fail under the tombstone.
 if _,e=admin.Exec(ctx,`UPDATE photo.collections SET revision=revision+1 WHERE tenant_id=$1 AND id=$2`,tid,collection);e!=nil{t.Fatal(e)}
 if _,e=svc.SaveEdits(ctx,who,tid,collection,key,req);domain.AsError(e).Code!="SAVE_EDITS_ABANDONED"{t.Fatal("late create not blocked",e)}
 if _,e=svc.ResolveSaveEdits(ctx,who,tid,other,key,req);domain.AsError(e).Code!="IDEMPOTENCY_CONFLICT"{t.Fatal("foreign parent",e)}
 altered:=req;altered.OutputName="Other";if _,e=svc.ResolveSaveEdits(ctx,who,tid,collection,key,altered);domain.AsError(e).Code!="IDEMPOTENCY_CONFLICT"{t.Fatal("altered body",e)}
 var expiry time.Time;if e=admin.QueryRow(ctx,`SELECT expires_at FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route='Run.SaveEdits' AND key=$3`,tid,actor,key).Scan(&expiry);e!=nil||expiry.Year()!=9999{t.Fatal("tombstone retention",expiry,e)}
 // Persist the exact accepted DTO and source-backed parent proof; no fabricated
 // arbitrary resource is allowed to win resolution.
 var snapshot,runID uuid.UUID
 if e=admin.QueryRow(ctx,`INSERT INTO photo.snapshots(tenant_id,kind,collection_id,source_revision,principal_type,principal_id,digest,item_count) VALUES($1,'selection',$2,1,'user',$3,$4,0) RETURNING id`,tid,collection,actor,strings.Repeat("a",64)).Scan(&snapshot);e!=nil{t.Fatal(e)}
 if e=admin.QueryRow(ctx,`INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,output_collection_id,config_snapshot,config_hash,created_by) VALUES($1,'apply','queued',$2,$3,'{}',$4,$5) RETURNING id`,tid,snapshot,output,strings.Repeat("b",64),actor).Scan(&runID);e!=nil{t.Fatal(e)}
 normalized,_,e:=saveEditsApplyRequest(req);if e!=nil{t.Fatal(e)};fingerprint:=struct{Source uuid.UUID `json:"source_collection_id"`;Request ApplyRequest `json:"request"`}{collection,normalized};hash,e:=canonical.Hash(fingerprint);if e!=nil{t.Fatal(e)}
 accepted:=ApplyAccepted{runID,output,"queued"};body,e:=canonical.Marshal(accepted);if e!=nil{t.Fatal(e)};acceptedKey:=uuid.NewString()
 if _,e=admin.Exec(ctx,`INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,response_status,response_body,resource_id,expires_at) VALUES($1,'user',$2,'Run.SaveEdits',$3,$4,202,$5::jsonb,$6,now()+interval '1 day')`,tid,actor,acceptedKey,hash,string(body),runID);e!=nil{t.Fatal(e)}
 resolved,e:=svc.ResolveSaveEdits(ctx,who,tid,collection,acceptedKey,req);if e!=nil||resolved.Status!=202||string(resolved.Body)!=string(body){t.Fatal("accepted wins unchanged",e)}
 var decoded ApplyAccepted;if json.Unmarshal(resolved.Body,&decoded)!=nil||decoded!=accepted{t.Fatal("actual DTO mismatch")}
 // Wait on the real receipt row, withdraw the source meanwhile, and ensure
 // resolution cannot deliver its old 202 using only the pre-wait collection read.
 holder,e:=admin.Begin(ctx);if e!=nil{t.Fatal(e)};defer holder.Rollback(context.WithoutCancel(ctx));var blocker int
 if e=holder.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&blocker);e!=nil{t.Fatal(e)}
 var held uuid.UUID;if e=holder.QueryRow(ctx,`SELECT id FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND route='Run.SaveEdits' AND key=$3 FOR UPDATE`,tid,actor,acceptedKey).Scan(&held);e!=nil{t.Fatal(e)}
 done:=make(chan error,1);go func(){_,err:=svc.ResolveSaveEdits(ctx,who,tid,collection,acceptedKey,req);done<-err}()
 timer:=time.NewTimer(3*time.Second);defer timer.Stop();tick:=time.NewTicker(10*time.Millisecond);defer tick.Stop();observed:=false
 for !observed{select{case err:=<-done:t.Fatal("receipt did not wait",err);case <-timer.C:t.Fatal("receipt lock wait missing");case <-tick.C:if e=admin.QueryRow(ctx,`SELECT EXISTS(SELECT FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`,blocker).Scan(&observed);e!=nil{t.Fatal(e)}}}
 if _,e=admin.Exec(ctx,`UPDATE photo.collections SET deleted_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2`,tid,collection);e!=nil{t.Fatal(e)};if e=holder.Commit(ctx);e!=nil{t.Fatal(e)}
 select{case e=<-done:if domain.AsError(e).Code!="NOT_FOUND"{t.Fatal("withdrawn source receipt exposed",e)};case <-ctx.Done():t.Fatal(ctx.Err())}
 if _,e=admin.Exec(ctx,`UPDATE photo.collections SET deleted_at=NULL WHERE tenant_id=$1 AND id=$2`,tid,collection);e!=nil{t.Fatal(e)}
 if _,e=admin.Exec(ctx,`UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1`,session);e!=nil{t.Fatal(e)}
 if _,e=svc.ResolveSaveEdits(ctx,who,tid,collection,acceptedKey,req);e==nil{t.Fatal("revoked session recovered receipt")}
}

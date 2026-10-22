package interchange

import (
 "context"
 "encoding/json"
 "os"
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
)

// The fixture uses a real current user/session/project/version and persisted
// granted decision. Revoke deliberately does not require the original import
// run to remain running or its proposal bytes to remain downloadable.
func TestImportReviewRevokeCurrentSessionAfterDecisionWait(t *testing.T){testImportReviewDecisionWait(t,false)}
func TestImportReviewGrantCurrentSessionAfterDecisionWait(t *testing.T){testImportReviewDecisionWait(t,true)}
func testImportReviewDecisionWait(t *testing.T,grant bool){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated migrated PostgreSQL required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 var actor,tenantID,project,artifact,asset,version,collection,snapshot,run,workflow uuid.UUID
 var epoch int64
 must:=func(q string,args ...any){t.Helper();if _,e:=db.Exec(ctx,q,args...);e!=nil{t.Fatal(e)}}
 scan:=func(q string,dst any,args ...any){t.Helper();if e:=db.QueryRow(ctx,q,args...).Scan(dst);e!=nil{t.Fatal(e)}}
 if err=db.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@test.example").Scan(&actor,&epoch);err!=nil{t.Fatal(err)}
 scan(`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,&tenantID,actor)
 session:=uuid.New();must(`INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',clock_timestamp()+interval '1 hour')`,session,actor,auth.HashSecret(uuid.NewString()),uuid.New())
 sha:=strings.Repeat("a",64)
 scan(`INSERT INTO photo.projects(tenant_id,name,visibility,download_allowed) VALUES($1,'Import decision','workspace',true) RETURNING id`,&project,tenantID)
 scan(`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role) VALUES($1,'photo',$2,'v1',$3,8,'image/jpeg','working_image') RETURNING id`,&artifact,tenantID,"review/"+uuid.NewString(),sha)
 scan(`INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`,&asset,tenantID,project,artifact)
 scan(`INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,'{}','accepted') RETURNING id`,&version,tenantID,asset,artifact,sha)
 scan(`INSERT INTO photo.collections(tenant_id,project_id,name,state) VALUES($1,$2,'Source','draft') RETURNING id`,&collection,tenantID,project)
 scan(`INSERT INTO photo.snapshots(tenant_id,kind,collection_id,source_revision,principal_type,principal_id,digest,item_count,expires_at) VALUES($1,'selection',$2,1,'user',$3,$4,0,clock_timestamp()+interval '1 hour') RETURNING id`,&snapshot,tenantID,collection,actor,sha)
 scan(`INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES($1,'v2','draft',$2,'{}',$3,$4,'fail_fast') RETURNING id`,&run,tenantID,snapshot,sha,actor)
 scan(`INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES($1,$2,$3,$4,'interchange','v2.2',$5) RETURNING id`,&workflow,tenantID,project,run,snapshot,sha)
 entry,proposal:=uuid.New(),uuid.New()
 proposalSHA:=sha;parsedRaw,proposalRaw:="",""
 if grant{version,proposal,proposalSHA,parsedRaw,proposalRaw=seedImportGrantSources(t,ctx,db,tenantID,project,actor,session,workflow,run,snapshot,asset,artifact,entry,sha)}
 body:=map[string]any{"action":"revoke","kind":"dust","expected_epoch":1,"entry_id":entry,"target_version_id":version,"proposal":map[string]any{"artifact_id":proposal,"sha256":proposalSHA},"base":map[string]any{"artifact_id":artifact,"sha256":sha}}
 if grant{body["action"]="grant";body["decision"]="skip"}
 request,_:=json.Marshal(body)
 seed:=func(){must(`INSERT INTO photo.v30_import_review_decisions(tenant_id,workflow_id,project_id,entry_id,target_version_id,kind,proposal_id,proposal_sha,base_id,base_sha,actor_id,request_id,decision,accepted_ids,rejected_ids,decision_hash,request_hash,status,epoch) VALUES($1,$2,$3,$4,$5,'dust',$6,$7,$8,$11,$9,$10,'skip','[]','[]',$11,$11,'granted',1)`,tenantID,workflow,project,entry,version,proposal,proposalSHA,artifact,actor,uuid.New(),sha)}
 seed()
 call:=func(tx pgx.Tx)(*int64,error){var got *int64;var e error
 if grant{e=tx.QueryRow(ctx,`SELECT photo.grant_v30_import_review($1,$2,$3,$4,$5,$6,$7,$8::jsonb)`,tenantID,actor,session,workflow,uuid.New(),parsedRaw,proposalRaw,request).Scan(&got)}else{e=tx.QueryRow(ctx,`SELECT photo.revoke_v30_import_review($1,$2,$3,$4,$5,$6::jsonb)`,tenantID,actor,session,workflow,uuid.New(),request).Scan(&got)};return got,e}
 // Establish a genuine authorized positive mutation before testing refusal.
 tx,e:=db.Begin(ctx);if e!=nil{t.Fatal(e)}
 if _,e=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tenantID.String());e!=nil{t.Fatal(e)}
 if _,e=tx.Exec(ctx,`SET LOCAL ROLE photo_api`);e!=nil{t.Fatal(e)}
 got,e:=call(tx);if e!=nil||got==nil||*got!=2{t.Fatal("positive revoke",got,e)}
 if e=tx.Rollback(ctx);e!=nil{t.Fatal(e)}
 blocker,e:=db.Begin(ctx);if e!=nil{t.Fatal(e)};defer blocker.Rollback(ctx)
 if _,e=blocker.Exec(ctx,`SELECT 1 FROM photo.v30_import_review_decisions WHERE tenant_id=$1 AND workflow_id=$2 FOR UPDATE`,tenantID,workflow);e!=nil{t.Fatal(e)}
 waiter,e:=db.Begin(ctx);if e!=nil{t.Fatal(e)};defer waiter.Rollback(ctx)
 if _,e=waiter.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tenantID.String());e!=nil{t.Fatal(e)}
 var pid int32;if e=waiter.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&pid);e!=nil{t.Fatal(e)}
 if _,e=waiter.Exec(ctx,`SET LOCAL ROLE photo_api`);e!=nil{t.Fatal(e)}
 type outcome struct{epoch *int64;err error};done:=make(chan outcome,1)
 go func(){n,e:=call(waiter);done<-outcome{n,e}}()
 ticker:=time.NewTicker(10*time.Millisecond);defer ticker.Stop()
 for{var waiting bool;scan(`SELECT cardinality(pg_blocking_pids($1))>0`,&waiting,pid);if waiting{break};select{case out:=<-done:t.Fatal("did not wait on decision",out);case <-ctx.Done():t.Fatal(ctx.Err());case <-ticker.C:}}
 // Expiry is changed on another physical connection, after initial authority
 // has succeeded and actual row-lock waiting has been observed.
 must(`UPDATE photo.sessions SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`,session)
 if e=blocker.Commit(ctx);e!=nil{t.Fatal(e)}
 out:=<-done;if out.err!=nil||out.epoch!=nil{t.Fatal("expired session mutated/replayed decision",out.epoch,out.err)}
 var state string;var currentEpoch int64
 if _,e=waiter.Exec(ctx,`RESET ROLE`);e!=nil{t.Fatal(e)}
 if e=waiter.QueryRow(ctx,`SELECT status,epoch FROM photo.v30_import_review_decisions WHERE tenant_id=$1 AND workflow_id=$2`,tenantID,workflow).Scan(&state,&currentEpoch);e!=nil{t.Fatal(e)}
 if state!="granted"||currentEpoch!=1{t.Fatal("refusal changed decision",state,currentEpoch)}
}

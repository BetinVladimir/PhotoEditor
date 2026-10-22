package review

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "io"
 "os"
 "reflect"
 "strings"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/objectstore"
)

type ordinaryDecisionObjects map[string][]byte
func (o ordinaryDecisionObjects) Open(ctx context.Context, object objectstore.Object)(io.ReadCloser,error){
 if err:=ctx.Err();err!=nil{return nil,err};return io.NopCloser(bytes.NewReader(o[object.Key])),nil
}

// Full ordinary Service.Decide, real migrated schema and photo_api role/RLS.
// Object bytes are in-memory exact fixture documents; no object store or worker
// execution is represented. Authored only during the coding phase.
func TestIntegrationOrdinaryDecideStoredViewAndDemotionAfterRunWait(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated fully migrated PostgreSQL required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 admin,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer admin.Close()
 cfg,err:=pgxpool.ParseConfig(dsn);if err!=nil{t.Fatal(err)};cfg.ConnConfig.RuntimeParams["role"]="photo_api";app:="ordinary-decide-"+uuid.NewString();cfg.ConnConfig.RuntimeParams["application_name"]=app
 api,err:=pgxpool.NewWithConfig(ctx,cfg);if err!=nil{t.Fatal(err)};defer api.Close()
 query:=func(sql string,args []any,dest ...any){t.Helper();if e:=admin.QueryRow(ctx,sql,args...).Scan(dest...);e!=nil{t.Fatal(e)}}
 exec:=func(sql string,args ...any){t.Helper();if _,e:=admin.Exec(ctx,sql,args...);e!=nil{t.Fatal(e)}}
 var owner,tid,project,collection uuid.UUID;var epoch int64;var who auth.Identity
 query(`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,[]any{uuid.NewString()+"@ordinary-owner.test"},&owner,&epoch)
 query(`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,[]any{owner},&tid)
 query(`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,[]any{uuid.NewString()+"@ordinary-editor.test"},&who.UserID,&who.Epoch)
 exec(`INSERT INTO photo.memberships(tenant_id,user_id,role) VALUES($1,$2,'editor')`,tid,who.UserID)
 query(`INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,'web',clock_timestamp()+interval '1 day') RETURNING id`,[]any{who.UserID,auth.HashSecret(uuid.NewString()),uuid.New()},&who.SessionID)
 query(`INSERT INTO photo.projects(tenant_id,name) VALUES($1,'Ordinary decision') RETURNING id`,[]any{tid},&project)
 query(`INSERT INTO photo.collections(tenant_id,project_id,name) VALUES($1,$2,'Decision source') RETURNING id`,[]any{tid,project},&collection)
 baseRaw:=imageFixtures(t)[0];candidate,err:=ParseWorkingImage(baseRaw);if err!=nil{t.Fatal(err)};candidate.Pixels.ID=uuid.New()
 candidateRaw,err:=canonical.Marshal(candidate);if err!=nil{t.Fatal(err)}
 objects:=ordinaryDecisionObjects{};checksum:=func(raw []byte)string{h:=sha256.Sum256(raw);return hex.EncodeToString(h[:])}
 baseSHA,candidateSHA:=checksum(baseRaw),checksum(candidateRaw)
 var base,params uuid.UUID
 query(`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,state,verified_at) VALUES($1,'photo',$2,'v1',$3,$4,'application/json','working_image','ready',now()) RETURNING id`,[]any{tid,"ordinary/base/"+uuid.NewString(),baseSHA,len(baseRaw)},&base)
 var baseKey string;query(`SELECT object_key FROM photo.artifacts WHERE tenant_id=$1 AND id=$2`,[]any{tid,base},&baseKey);objects[baseKey]=baseRaw
 query(`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,state,verified_at) VALUES($1,'photo',$2,'v1',$3,2,'application/json','task_params','ready',now()) RETURNING id`,[]any{tid,uuid.NewString(),strings.Repeat("a",64)},&params)
 fixture:=func()(uuid.UUID,uuid.UUID){t.Helper();var snapshot,run,task,output,result,review uuid.UUID
  query(`INSERT INTO photo.snapshots(tenant_id,kind,collection_id,source_revision,principal_type,principal_id,digest,item_count) VALUES($1,'selection',$2,1,'user',$3,$4,0) RETURNING id`,[]any{tid,collection,who.UserID,baseSHA},&snapshot)
  query(`INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES($1,'apply','waiting_review',$2,'{}',$3,$4,true) RETURNING id`,[]any{tid,snapshot,baseSHA,who.UserID},&run)
  query(`INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,attempt,fencing_token,execution_profile) VALUES($1,$2,'inpaint','image.inpaint','1','gpu','ready',jsonb_build_array(jsonb_build_object('artifact_id',$3::text,'sha256',$4::text)),$5,$4,1,1,'{}') RETURNING id`,[]any{tid,run,base,baseSHA,params},&task)
  key:="ordinary/candidate/"+uuid.NewString();objects[key]=candidateRaw
  query(`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,state,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES($1,'photo',$2,'v1',$3,$4,'application/json','working_image','ready',now(),$5,1,1) RETURNING id`,[]any{tid,key,candidateSHA,len(candidateRaw),task},&output)
  query(`INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES($1,$2,1,1,jsonb_build_array(jsonb_build_object('artifact_id',$3::text,'sha256',$4::text)),'{}',$4) RETURNING id`,[]any{tid,task,output,candidateSHA},&result)
  exec(`UPDATE photo.tasks SET canonical_result_id=$3,state='succeeded' WHERE tenant_id=$1 AND id=$2`,tid,task,result)
  query(`INSERT INTO photo.reviews(tenant_id,run_id,task_id,candidate_artifact_id,candidate_hash,base_artifact_id,kind,reasons) VALUES($1,$2,$3,$4,$5,$6,'inpaint','["photographer-review"]') RETURNING id`,[]any{tid,run,task,output,candidateSHA,base},&review)
  return run,review
 }
 gate:=&testGate{};service:=Service{DB:api,Objects:objects,Bucket:"photo",Gate:gate}
 run,review:=fixture();requestID:=uuid.New();response,err:=service.Decide(ctx,who,tid,review,requestID,Decision{ExpectedRevision:1,CandidateHash:candidateSHA,Decision:"accept"});if err!=nil{t.Fatal(err)}
 var stored View;query(`SELECT id,run_id,kind,state,revision,candidate_hash,decision,decision_route,decision_hash,reasons,created_at FROM photo.reviews WHERE tenant_id=$1 AND id=$2`,[]any{tid,review},&stored.ID,&stored.RunID,&stored.Kind,&stored.State,&stored.Revision,&stored.CandidateHash,&stored.Decision,&stored.DecisionRoute,&stored.DecisionHash,&stored.Reasons,&stored.CreatedAt)
 if !reflect.DeepEqual(response,stored)||response.RunID!=run||response.CreatedAt.IsZero()||response.DecisionHash==nil||*response.DecisionHash==""||response.DecisionRoute==nil||*response.DecisionRoute!="candidate"||response.Revision!=2||!json.Valid(response.Reasons){t.Fatal("response must equal complete durable View",response,stored)}
 var auditCount int;query(`SELECT count(*) FROM photo.audit_log WHERE tenant_id=$1 AND resource_id=$2 AND request_id=$3 AND action='review.decide'`,[]any{tid,review,requestID},&auditCount);if auditCount!=1||gate.calls!=0{t.Fatal("ordinary branch audit/gate",auditCount,gate.calls)}
 // Second decision has passed initial and transaction project authority, then
 // blocks on the actual run lock. Demotion while waiting must roll back the
 // candidate decision and its audit at the post-persistence authority fence.
 racingRun,racingReview:=fixture();blocker,err:=admin.Begin(ctx);if err!=nil{t.Fatal(err)};defer blocker.Rollback(context.WithoutCancel(ctx))
 if _,err=blocker.Exec(ctx,`SELECT id FROM photo.runs WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,tid,racingRun);err!=nil{t.Fatal(err)}
 racingRequest:=uuid.New();done:=make(chan error,1);go func(){_,e:=service.Decide(ctx,who,tid,racingReview,racingRequest,Decision{ExpectedRevision:1,CandidateHash:candidateSHA,Decision:"accept"});done<-e}()
 deadline:=time.Now().Add(3*time.Second);for{var waiting bool;query(`SELECT EXISTS(SELECT FROM pg_stat_activity WHERE application_name=$1 AND cardinality(pg_blocking_pids(pid))>0 AND query LIKE '%FOR UPDATE OF r%')`,[]any{app},&waiting);if waiting{break};select{case e:=<-done:t.Fatal("decision did not reach actual run wait",e);default:};if time.Now().After(deadline){t.Fatal("decision failed to wait on run")};time.Sleep(10*time.Millisecond)}
 exec(`UPDATE photo.memberships SET role='viewer',revision=revision+1 WHERE tenant_id=$1 AND user_id=$2`,tid,who.UserID)
 if err=blocker.Commit(ctx);err!=nil{t.Fatal(err)};if err=<-done;err==nil||domain.AsError(err).Code!="FORBIDDEN"{t.Fatal("expected post-wait authority refusal",err)}
 var state string;var revision int64;query(`SELECT state,revision FROM photo.reviews WHERE tenant_id=$1 AND id=$2`,[]any{tid,racingReview},&state,&revision);query(`SELECT count(*) FROM photo.audit_log WHERE tenant_id=$1 AND resource_id=$2 AND request_id=$3 AND action='review.decide'`,[]any{tid,racingReview,racingRequest},&auditCount)
 if state!="pending"||revision!=1||auditCount!=0{t.Fatal("demotion must roll back decision and audit",state,revision,auditCount)}
}

package interactive

import (
 "bytes"
 "context"
 "encoding/json"
 "os"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
)

// Uses a real API transaction and the actual Preview producer, with a ready
// source and a qualified media registration. No renderer execution is asserted.
func TestIntegrationPreviewAcceptedReplayAfterDraftStyleAndWorkerChanges(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated fully migrated PostgreSQL required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 admin,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer admin.Close()
 cfg,err:=pgxpool.ParseConfig(dsn);if err!=nil{t.Fatal(err)};cfg.ConnConfig.RuntimeParams["role"]="photo_api"
 api,err:=pgxpool.NewWithConfig(ctx,cfg);if err!=nil{t.Fatal(err)};defer api.Close()
 var actor,tid,session,project,artifact,asset,version,style,styleVersion uuid.UUID;var epoch int64
 must:=func(e error){t.Helper();if e!=nil{t.Fatal(e)}}
 must(admin.QueryRow(ctx,"SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)",uuid.NewString()+"@preview.test").Scan(&actor,&epoch))
 must(admin.QueryRow(ctx,"SELECT id FROM photo.tenants WHERE owner_user_id=$1",actor).Scan(&tid))
 must(admin.QueryRow(ctx,"INSERT INTO photo.sessions(user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,'web',now()+interval '1 day') RETURNING id",actor,auth.HashSecret(uuid.NewString()),uuid.New()).Scan(&session))
 must(admin.QueryRow(ctx,"INSERT INTO photo.projects(tenant_id,name) VALUES($1,'Preview recovery') RETURNING id",tid).Scan(&project))
 sha:=strings.Repeat("a",64)
 must(admin.QueryRow(ctx,`INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,state,verified_at) VALUES($1,'photo',$2,'v1',$3,100,'image/jpeg','working_image','ready',now()) RETURNING id`,tid,uuid.NewString(),sha).Scan(&artifact))
 must(admin.QueryRow(ctx,`INSERT INTO photo.assets(tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance) VALUES($1,$2,$3,'{}','verified','{}') RETURNING id`,tid,project,artifact).Scan(&asset))
 must(admin.QueryRow(ctx,`INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES($1,$2,$3,$4,'{}','accepted') RETURNING id`,tid,asset,artifact,sha).Scan(&version))
 must(admin.QueryRow(ctx,"INSERT INTO photo.styles(tenant_id,project_id,name,created_by) VALUES($1,$2,'Fixed',$3) RETURNING id",tid,project,actor).Scan(&style))
 must(admin.QueryRow(ctx,`INSERT INTO photo.style_versions(tenant_id,style_id,version_no,config_json,config_hash,schema_version,renderer_version,approval_status,metrics,export_artifact_id) VALUES($1,$2,1,'{"mode":"fixed"}',$3,'2.0.0','photo_renderer_v2','accepted','{}',$4) RETURNING id`,tid,style,sha,artifact).Scan(&styleVersion))
 _,err=admin.Exec(ctx,`INSERT INTO photo.editor_drafts(tenant_id,asset_version_id,user_id,params,mask_refs) VALUES($1,$2,$3,'{"strength":1}','[]')`,tid,version,actor);must(err)
 node:="preview-"+uuid.NewString();worker:=uuid.New()
 _,err=admin.Exec(ctx,`INSERT INTO photo.resource_nodes(node_id,enabled,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes,ram_available_bytes,scratch_available_bytes,reported_at,doctor_report_sha256) VALUES($1,true,8589934592,8589934592,8589934592,8589934592,8589934592,clock_timestamp(),$2)`,node,sha);must(err)
 _,err=admin.Exec(ctx,`INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations) VALUES($1,$2,'["media"]','["cpu"]',$3,clock_timestamp(),'{}','photo_worker',$4,true,'{"editor.preview":["cpu"]}')`,tid,worker,sha,node);must(err)
 who:=auth.Identity{UserID:actor,SessionID:session,Epoch:epoch};service:=Service{DB:api}
 request:=PreviewRequest{AssetVersionID:version,DraftRevision:1,StyleVersionID:&styleVersion,Params:json.RawMessage(`{"strength":1}`),Scale:"proxy"};key:=uuid.NewString()
 first,err:=service.Preview(ctx,who,tid,key,request);must(err);if first.Status!=202||first.Replayed||first.ResourceID==nil{t.Fatal("fresh Preview did not produce a receipt",first)}
 var initialTasks,initialRuns int
 must(admin.QueryRow(ctx,"SELECT count(*) FROM photo.interactive_tasks WHERE tenant_id=$1",tid).Scan(&initialTasks))
 must(admin.QueryRow(ctx,"SELECT count(*) FROM photo.runs WHERE tenant_id=$1",tid).Scan(&initialRuns))
 _,err=admin.Exec(ctx,"UPDATE photo.editor_drafts SET revision=revision+1 WHERE tenant_id=$1 AND asset_version_id=$2 AND user_id=$3",tid,version,actor);must(err)
 _,err=admin.Exec(ctx,"UPDATE photo.styles SET archived_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2",tid,style);must(err)
 _,err=admin.Exec(ctx,"UPDATE photo.worker_registrations SET enabled=false WHERE tenant_id=$1 AND worker_id=$2",tid,worker);must(err)
 replay,err:=service.Preview(ctx,who,tid,key,request);must(err)
 if !replay.Replayed||replay.Status!=202||!bytes.Equal(first.Body,replay.Body)||replay.ResourceID==nil||*first.ResourceID!=*replay.ResourceID{t.Fatal("accepted receipt lost after mutable changes",replay)}
 changed:=request;changed.Scale="full"
 if _,e:=service.Preview(ctx,who,tid,key,changed);e==nil||domain.AsError(e).Code!="IDEMPOTENCY_CONFLICT"{t.Fatal("changed body did not conflict",e)}
 if _,e:=service.Preview(ctx,who,tid,uuid.NewString(),request);e==nil||domain.AsError(e).Code!="REVISION_CONFLICT"{t.Fatal("fresh request bypassed revision",e)}
 fresh:=request;fresh.DraftRevision=2
 if _,e:=service.Preview(ctx,who,tid,uuid.NewString(),fresh);e==nil||domain.AsError(e).Code!="NOT_FOUND"{t.Fatal("fresh request bypassed archived style",e)}
 fresh.StyleVersionID=nil
 if _,e:=service.Preview(ctx,who,tid,uuid.NewString(),fresh);e==nil||domain.AsError(e).Code!="OPERATION_UNAVAILABLE"{t.Fatal("fresh request bypassed worker refusal",e)}
 _,err=admin.Exec(ctx,"UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1",session);must(err)
 if _,e:=service.Preview(ctx,who,tid,key,request);e==nil{t.Fatal("revoked session recovered protected receipt")}
 var tasks,runs int
 must(admin.QueryRow(ctx,"SELECT count(*) FROM photo.interactive_tasks WHERE tenant_id=$1",tid).Scan(&tasks));must(admin.QueryRow(ctx,"SELECT count(*) FROM photo.runs WHERE tenant_id=$1",tid).Scan(&runs))
 if tasks!=initialTasks||runs!=initialRuns{t.Fatal("replay/refusal created duplicate work",initialTasks,tasks,initialRuns,runs)}
}

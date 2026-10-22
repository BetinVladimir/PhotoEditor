package run

import (
 "context"
 "os"
 "strings"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/pagination"
)

// Actual API-role reads; fixture writes deliberately use the isolated test admin.
// Authored only: no execution evidence or physical producer qualification.
func TestIntegrationRunReadRequiresEveryCurrentProject(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated current migrated PostgreSQL required")}
 ctx,cancel:=context.WithTimeout(context.Background(),30*time.Second);defer cancel()
 admin,e:=pgxpool.New(ctx,dsn);if e!=nil{t.Fatal(e)};defer admin.Close()
 cfg,e:=pgxpool.ParseConfig(dsn);if e!=nil{t.Fatal(e)};cfg.ConnConfig.RuntimeParams["role"]="photo_api"
 api,e:=pgxpool.NewWithConfig(ctx,cfg);if e!=nil{t.Fatal(e)};defer api.Close()
 var owner,user,tid uuid.UUID;var epoch int64
 if e=admin.QueryRow(ctx,`SELECT user_id FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@test.example").Scan(&owner);e!=nil{t.Fatal(e)}
 if e=admin.QueryRow(ctx,`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,owner).Scan(&tid);e!=nil{t.Fatal(e)}
 if e=admin.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@test.example").Scan(&user,&epoch);e!=nil{t.Fatal(e)}
 if _,e=admin.Exec(ctx,`INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES($1,$2,'viewer','active')`,tid,user);e!=nil{t.Fatal(e)}
 session:=uuid.New();if _,e=admin.Exec(ctx,`INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`,session,user,auth.HashSecret(uuid.NewString()),uuid.New());e!=nil{t.Fatal(e)}
 project:=func(name,visibility string)uuid.UUID{var id uuid.UUID;if e:=admin.QueryRow(ctx,`INSERT INTO photo.projects(tenant_id,name,visibility) VALUES($1,$2,$3) RETURNING id`,tid,name,visibility).Scan(&id);e!=nil{t.Fatal(e)};return id}
 open,restricted:=project("Open","workspace"),project("Restricted","restricted")
 var snapshot uuid.UUID;if e=admin.QueryRow(ctx,`INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES($1,'input','user',$2,$3,0) RETURNING id`,tid,owner,strings.Repeat("a",64)).Scan(&snapshot);e!=nil{t.Fatal(e)}
 create:=func(p *uuid.UUID)uuid.UUID{var id uuid.UUID;config:="{}";if p!=nil{config=`{"project_id":"`+p.String()+`"}`};if e:=admin.QueryRow(ctx,`INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES($1,'preview','queued',$2,$3::jsonb,$4,$5) RETURNING id`,tid,snapshot,config,strings.Repeat("b",64),owner).Scan(&id);e!=nil{t.Fatal(e)};return id}
 hidden,visible,orphan:=create(&restricted),create(&open),create(nil)
 who:=auth.Identity{UserID:user,SessionID:session,Epoch:epoch};svc:=Service{DB:api,Cursors:pagination.Codec{Key:[]byte(strings.Repeat("k",32))}}
 if _,e=svc.Get(ctx,who,tid,hidden);e==nil{t.Fatal("restricted summary exposed")}
 if _,e=svc.Get(ctx,who,tid,orphan);e==nil{t.Fatal("unknown orphan exposed")}
 if _,e=svc.Get(ctx,who,tid,visible);e!=nil{t.Fatal(e)}
 page,e:=svc.List(ctx,who,tid,1,"","");if e!=nil||len(page.Items)!=1||page.Items[0].ID!=visible||page.NextCursor!=nil{t.Fatal("visibility must precede page limit",page,e)}
 if _,e=admin.Exec(ctx,`INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'viewer')`,tid,restricted,user);e!=nil{t.Fatal(e)}
 if _,e=svc.Get(ctx,who,tid,hidden);e!=nil{t.Fatal("Viewer grant must allow reads",e)}
 if _,e=svc.Cancel(ctx,who,tid,hidden,uuid.NewString());e==nil{t.Fatal("Viewer read must not elevate mutation authority")}
 var collection uuid.UUID;if e=admin.QueryRow(ctx,`INSERT INTO photo.collections(tenant_id,project_id,name) VALUES($1,$2,'Second lane') RETURNING id`,tid,restricted).Scan(&collection);e!=nil{t.Fatal(e)}
 if _,e=admin.Exec(ctx,`UPDATE photo.runs SET output_collection_id=$3 WHERE tenant_id=$1 AND id=$2`,tid,visible,collection);e!=nil{t.Fatal(e)}
 if _,e=admin.Exec(ctx,`DELETE FROM photo.project_acl WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3`,tid,restricted,user);e!=nil{t.Fatal(e)}
 if _,e=svc.Get(ctx,who,tid,visible);e==nil{t.Fatal("config open project must not hide restricted output project")}
 page,e=svc.List(ctx,who,tid,100,"","");if e!=nil||len(page.Items)!=0{t.Fatal("revoked multi-lane run remains visible",page,e)}
 var style uuid.UUID;if e=admin.QueryRow(ctx,`INSERT INTO photo.styles(tenant_id,project_id,name,created_by) VALUES($1,$2,'Read source',$3) RETURNING id`,tid,open,owner).Scan(&style);e!=nil{t.Fatal(e)}
 styleRun:=create(nil);if _,e=admin.Exec(ctx,`UPDATE photo.runs SET config_snapshot=jsonb_build_object('style_id',$3::text) WHERE tenant_id=$1 AND id=$2`,tid,styleRun,style);e!=nil{t.Fatal(e)}
 outputs,e:=svc.readOutputCandidates(ctx,who,tid,styleRun);if e!=nil||len(outputs)!=0{t.Fatal("style-only source must support empty output read",outputs,e)}
 ingest:=create(nil);upload:=uuid.New();var reservation uuid.UUID
 if _,e=admin.Exec(ctx,`UPDATE photo.runs SET kind='ingest' WHERE tenant_id=$1 AND id=$2`,tid,ingest);e!=nil{t.Fatal(e)}
 if e=admin.QueryRow(ctx,`INSERT INTO photo.quota_reservations(tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,state,expires_at) VALUES($1,'user',$2,'upload',$3,3,1,'reserved',now()+interval '1 hour') RETURNING id`,tid,owner,upload).Scan(&reservation);e!=nil{t.Fatal(e)}
 if _,e=admin.Exec(ctx,`INSERT INTO photo.upload_sessions(id,tenant_id,project_id,principal_type,principal_id,expected_size,filename,object_key,expires_at,reservation_id,ingest_run_id,ingest_snapshot_id,staging_candidate_id) VALUES($1,$2,$3,'user',$4,3,'frame.jpg',$5,now()+interval '1 hour',$6,$7,$8,$9)`,upload,tid,open,owner,"test/"+upload.String(),reservation,ingest,snapshot,uuid.New());e!=nil{t.Fatal(e)}
 outputs,e=svc.readOutputCandidates(ctx,who,tid,ingest);if e!=nil||len(outputs)!=0{t.Fatal("ingest-only source must support empty output read",outputs,e)}
 first,e:=svc.List(ctx,who,tid,1,"","");if e!=nil||len(first.Items)!=1||first.NextCursor==nil{t.Fatal("first real cursor page",first,e)}
 second,e:=svc.List(ctx,who,tid,1,*first.NextCursor,"");if e!=nil||len(second.Items)!=1||second.Items[0].ID==first.Items[0].ID{t.Fatal("second real cursor page",second,e)}
 malformed:=create(&open);if _,e=admin.Exec(ctx,`UPDATE photo.runs SET config_snapshot=config_snapshot||'{"asset_version_id":"not-a-uuid"}'::jsonb WHERE tenant_id=$1 AND id=$2`,tid,malformed);e!=nil{t.Fatal(e)}
 if _,e=svc.Get(ctx,who,tid,malformed);domain.AsError(e).Code!="NOT_FOUND"{t.Fatal("malformed locator must refuse without unsafe SQL cast",e)}
 if _,e=admin.Exec(ctx,`UPDATE photo.memberships SET role='editor' WHERE tenant_id=$1 AND user_id=$2`,tid,user);e!=nil{t.Fatal(e)}
 if _,e=admin.Exec(ctx,`INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'viewer')`,tid,restricted,user);e!=nil{t.Fatal(e)}
 // Config and output are open, but a second input project is restricted. The
 // former COALESCE picked output and skipped this project's Editor gate.
 var mixedSnapshot,mixedOutput uuid.UUID
 if e=admin.QueryRow(ctx,`INSERT INTO photo.snapshots(tenant_id,kind,collection_id,principal_type,principal_id,digest,item_count) VALUES($1,'input',$2,'user',$3,$4,0) RETURNING id`,tid,collection,owner,strings.Repeat("c",64)).Scan(&mixedSnapshot);e!=nil{t.Fatal(e)}
 if e=admin.QueryRow(ctx,`INSERT INTO photo.collections(tenant_id,project_id,name) VALUES($1,$2,'Open output') RETURNING id`,tid,open).Scan(&mixedOutput);e!=nil{t.Fatal(e)}
 mixed:=create(&open);if _,e=admin.Exec(ctx,`UPDATE photo.runs SET input_snapshot_id=$3,output_collection_id=$4 WHERE tenant_id=$1 AND id=$2`,tid,mixed,mixedSnapshot,mixedOutput);e!=nil{t.Fatal(e)}
 if _,e=svc.Get(ctx,who,tid,mixed);e!=nil{t.Fatal("Viewer project grant permits mixed-project reads",e)}
 key:=uuid.NewString();if _,e=svc.Cancel(ctx,who,tid,mixed,key);domain.AsError(e).Code!="FORBIDDEN"{t.Fatal("all input projects need Editor before cancellation",e)}
 if _,e=svc.Retry(ctx,who,tid,mixed,uuid.NewString(),RetryRequest{ExpectedRevision:"1"});domain.AsError(e).Code!="FORBIDDEN"{t.Fatal("retry must authorize all projects before state inspection",e)}
 if _,e=admin.Exec(ctx,`UPDATE photo.project_acl SET role='editor' WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3`,tid,restricted,user);e!=nil{t.Fatal(e)}
 accepted,e:=svc.Cancel(ctx,who,tid,mixed,key);if e!=nil||accepted.State!="cancelling"{t.Fatal("all-project Editor cancellation",accepted,e)}
 replay,e:=svc.Cancel(ctx,who,tid,mixed,key);if e!=nil||replay.Revision!=accepted.Revision{t.Fatal("exact accepted replay",replay,e)}
 waiting:=create(&restricted);waitKey:=uuid.NewString()
 holder,e:=admin.Begin(ctx);if e!=nil{t.Fatal(e)};defer holder.Rollback(ctx)
 var blocker int;if e=holder.QueryRow(ctx,`SELECT pg_backend_pid()`).Scan(&blocker);e!=nil{t.Fatal(e)}
 var held uuid.UUID;if e=holder.QueryRow(ctx,`SELECT id FROM photo.runs WHERE tenant_id=$1 AND id=$2 FOR UPDATE`,tid,waiting).Scan(&held);e!=nil{t.Fatal(e)}
 done:=make(chan error,1);go func(){_,err:=svc.Cancel(ctx,who,tid,waiting,waitKey);done<-err}()
 deadline:=time.NewTimer(3*time.Second);defer deadline.Stop();tick:=time.NewTicker(10*time.Millisecond);defer tick.Stop()
 observed:=false
 for !observed {select{case err:=<-done:t.Fatal("operation did not wait on actual run lock",err);case <-deadline.C:t.Fatal("run lock wait not observed");case <-tick.C:if e=admin.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)))`,blocker).Scan(&observed);e!=nil{t.Fatal(e)}}}
 if _,e=admin.Exec(ctx,`DELETE FROM photo.project_acl WHERE tenant_id=$1 AND project_id=$2 AND user_id=$3`,tid,restricted,user);e!=nil{t.Fatal(e)}
 if e=holder.Commit(ctx);e!=nil{t.Fatal(e)}
 select{case e=<-done:if e==nil{t.Fatal("revoked project committed after run lock wait")};case <-ctx.Done():t.Fatal(ctx.Err())}
 var state string;var cancelled bool;var receipts int
 if e=admin.QueryRow(ctx,`SELECT status,cancel_requested FROM photo.runs WHERE tenant_id=$1 AND id=$2`,tid,waiting).Scan(&state,&cancelled);e!=nil||state!="queued"||cancelled{t.Fatal("revoked cancellation effects not rolled back",state,cancelled,e)}
 if e=admin.QueryRow(ctx,`SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_id=$2 AND key=$3`,tid,user,waitKey).Scan(&receipts);e!=nil||receipts!=0{t.Fatal("revoked operation persisted receipt",receipts,e)}
 if _,e=admin.Exec(ctx,`INSERT INTO photo.project_acl(tenant_id,project_id,user_id,role) VALUES($1,$2,$3,'editor')`,tid,restricted,user);e!=nil{t.Fatal(e)}
 if _,e=admin.Exec(ctx,`UPDATE photo.projects SET archived_at=clock_timestamp() WHERE tenant_id=$1 AND id=$2`,tid,restricted);e!=nil{t.Fatal(e)}
 if _,e=svc.Get(ctx,who,tid,mixed);domain.AsError(e).Code!="NOT_FOUND"{t.Fatal("archived input project must revoke reads",e)}
 if _,e=svc.Cancel(ctx,who,tid,mixed,key);domain.AsError(e).Code!="NOT_FOUND"{t.Fatal("saved receipt must not bypass current archived-project authority",e)}
}

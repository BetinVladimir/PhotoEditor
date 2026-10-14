package interchange

import (
 "context"
 "os"
 "testing"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
)
func TestIntegrationOpenBeginInspectionScopeAndReadOnly(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated PostgreSQL with01775 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 signature:="photo.inspect_v30_archive_open_begin(uuid,uuid,uuid,uuid,uuid)"
 for _,role:=range []string{"photo_api","photo_worker","photo_scheduler","photo_gateway","photo_portal"}{
  var allowed bool;if err=db.QueryRow(ctx,`SELECT has_function_privilege($1,$2,'EXECUTE')`,role,signature).Scan(&allowed);err!=nil{t.Fatal(err)}
  if allowed!=(role=="photo_api"){t.Fatal("inspection grant escaped API",role)}
 }
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 var actor,tid,project,session uuid.UUID;var epoch int64
 if err=tx.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@inspection.test").Scan(&actor,&epoch);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,actor).Scan(&tid);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`INSERT INTO photo.projects(tenant_id,name) VALUES($1,'Inspection') RETURNING id`,tid).Scan(&project);err!=nil{t.Fatal(err)}
 session=uuid.New()
 if _,err=tx.Exec(ctx,`INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`,session,actor,auth.HashSecret(uuid.NewString()),uuid.New());err!=nil{t.Fatal(err)}
 who:=auth.Identity{UserID:actor,SessionID:session,Epoch:epoch}
 intent,err:=FreezeArchiveUpload(ctx,tid,who,"inspect-open",ArchiveUploadRequest{ProjectID:project,Filename:"inspection.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))});if err!=nil{t.Fatal(err)}
 if _,err=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tid.String());err!=nil{t.Fatal(err)}
 if _,err=tx.Exec(ctx,`SET LOCAL ROLE photo_api`);err!=nil{t.Fatal(err)}
 var outcome string;var upload,token *uuid.UUID;var state,multipart *string
 if err=tx.QueryRow(ctx,`SELECT outcome,upload_id,token,state,multipart_id FROM photo.reserve_v30_archive_upload($1,$2,$3,$4,$5,'photos')`,tid,actor,session,"inspect-open",string(intent.RequestBody())).Scan(&outcome,&upload,&token,&state,&multipart);err!=nil{t.Fatal(err)}
 if outcome!="created"||upload==nil||*upload!=intent.ID()||token==nil{t.Fatal("fixture reserve failed",outcome)}
 var writer *uuid.UUID
 if err=tx.QueryRow(ctx,`SELECT photo.start_v30_archive_begin($1,$2,$3,$4,$5)`,tid,actor,session,*upload,*token).Scan(&writer);err!=nil||writer==nil{t.Fatal("fixture writer failed",err)}
 for _,mode:=range []string{"valid","foreign tenant","foreign actor","foreign session","foreign upload","foreign reservation"}{t.Run(mode,func(t *testing.T){
  args:=[]any{tid,actor,session,*upload,*token}
  switch mode{case "foreign tenant":args[0]=uuid.New();case "foreign actor":args[1]=uuid.New();case "foreign session":args[2]=uuid.New();case "foreign upload":args[3]=uuid.New();case "foreign reservation":args[4]=uuid.New()}
  var observed *uuid.UUID
  if err:=tx.QueryRow(ctx,`SELECT photo.inspect_v30_archive_open_begin($1,$2,$3,$4,$5)`,args...).Scan(&observed);err!=nil{t.Fatal(err)}
  if mode=="valid"{if observed==nil||*observed!=*writer{t.Fatal("open writer not visible")}}else if observed!=nil{t.Fatal("foreign scope exposed writer")}
 })}
 if _,err=tx.Exec(ctx,`RESET ROLE`);err!=nil{t.Fatal(err)}
 var writerState,uploadState string;var receipt *string
 if err=tx.QueryRow(ctx,`SELECT w.state,w.receipt,u.state FROM photo.v30_archive_writers w JOIN photo.v30_archive_uploads u ON u.tenant_id=w.tenant_id AND u.id=w.upload_id WHERE w.tenant_id=$1 AND w.upload_id=$2 AND w.token=$3`,tid,*upload,*writer).Scan(&writerState,&receipt,&uploadState);err!=nil{t.Fatal(err)}
 if writerState!="open"||receipt!=nil||uploadState!="reserved"{t.Fatal("inspection mutated writer/upload",writerState,receipt,uploadState)}
 // The same pending Begin must fence cleanup even if the upload has expired.
 // Work inside the rollback-only fixture; no object-store operation is issued.
 cleanup:=uuid.New()
 if _,err=tx.Exec(ctx,`UPDATE photo.v30_archive_uploads SET state='aborting',error_code='UPLOAD_EXPIRED',expires_at=clock_timestamp()-interval '1 day',cleanup_not_before=clock_timestamp()-interval '1 minute',cleanup_token=$3,cleanup_lease_until=clock_timestamp()+interval '1 minute',completion_lease_until=NULL,parts_authorized_until=NULL WHERE tenant_id=$1 AND id=$2`,tid,*upload,cleanup);err!=nil{t.Fatal(err)}
 var allowed bool
 if err=tx.QueryRow(ctx,`SELECT photo.v30_archive_cleanup_authorized($1,$2,$3,'photos',$4)`,tid,*upload,cleanup,intent.ObjectKey()).Scan(&allowed);err!=nil{t.Fatal(err)}
 if allowed{t.Fatal("open Begin writer authorized destructive cleanup")}
 // A rollback-only deletion job must also retain the open writer, regardless
 // of a temporarily empty storage listing reported by maintenance.
 job,finishToken:=uuid.New(),uuid.New()
 if _,err=tx.Exec(ctx,`INSERT INTO photo.deletion_jobs(id,tenant_id,resource_type,resource_id,status,requested_by,cleanup_token,cleanup_lease_until) VALUES($1,$2,'tenant',$2,'running',$3,$4,clock_timestamp()+interval '1 minute')`,job,tid,actor,finishToken);err!=nil{t.Fatal(err)}
 if _,err=tx.Exec(ctx,`UPDATE photo.tenants SET status='deleting' WHERE id=$1`,tid);err!=nil{t.Fatal(err)}
 var finished bool
 if err=tx.QueryRow(ctx,`SELECT photo.finish_tenant_deletion($1,$2)`,job,finishToken).Scan(&finished);err!=nil{t.Fatal(err)}
 if finished{t.Fatal("tenant metadata purged while Archive Begin writer is open")}
 if _,err=tx.Exec(ctx,`UPDATE photo.tenants SET status='active' WHERE id=$1`,tid);err!=nil{t.Fatal(err)}
 if _,err=tx.Exec(ctx,`DELETE FROM photo.v30_archive_writers WHERE tenant_id=$1 AND upload_id=$2 AND token=$3`,tid,*upload,*writer);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`SELECT photo.v30_archive_cleanup_authorized($1,$2,$3,'photos',$4)`,tid,*upload,cleanup,intent.ObjectKey()).Scan(&allowed);err!=nil{t.Fatal(err)}
 if !allowed{t.Fatal("closed writer fixture did not reach cleanup authority")}
 if _,err=tx.Exec(ctx,`UPDATE photo.tenants SET status='deleting' WHERE id=$1`,tid);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`SELECT photo.finish_tenant_deletion($1,$2)`,job,finishToken).Scan(&finished);err!=nil{t.Fatal(err)}
 if !finished{t.Fatal("resolved archive writer still blocked metadata finish")}
}

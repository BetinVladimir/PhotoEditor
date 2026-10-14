package interchange

import (
 "context"
 "errors"
 "os"
 "testing"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
)

// The rollback-only fixture exercises actual Reserve transitions, not a source
// string search. It requires an isolated database migrated through 01931.
func TestIntegrationArchiveCrossKeyBeginFence(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated PostgreSQL through migration 01931 required")}
 ctx,cancel:=context.WithTimeout(context.Background(),20*time.Second);defer cancel()
 db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 tx,err:=db.Begin(ctx);if err!=nil{t.Fatal(err)};defer tx.Rollback(ctx)
 var actor,tenantID,project,session uuid.UUID;var epoch int64
 if err=tx.QueryRow(ctx,`SELECT user_id,auth_epoch FROM photo.auth_bootstrap_user($1,'en',true)`,uuid.NewString()+"@archive-cross-key.test").Scan(&actor,&epoch);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`SELECT id FROM photo.tenants WHERE owner_user_id=$1`,actor).Scan(&tenantID);err!=nil{t.Fatal(err)}
 if err=tx.QueryRow(ctx,`INSERT INTO photo.projects(tenant_id,name) VALUES($1,'Archive cross key') RETURNING id`,tenantID).Scan(&project);err!=nil{t.Fatal(err)}
 session=uuid.New()
 if _,err=tx.Exec(ctx,`INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`,session,actor,auth.HashSecret(uuid.NewString()),uuid.New());err!=nil{t.Fatal(err)}
 who:=auth.Identity{UserID:actor,SessionID:session,Epoch:epoch}
 request:=ArchiveUploadRequest{ProjectID:project,Filename:"original.zip",Bytes:22,SHA256:rawSHA([]byte("archive"))}
 first,err:=FreezeArchiveUpload(ctx,tenantID,who,"first-key",request);if err!=nil{t.Fatal(err)}
 renamed:=request;renamed.Filename="renamed.zip"
 second,err:=FreezeArchiveUpload(ctx,tenantID,who,"second-key",renamed);if err!=nil{t.Fatal(err)}
 if _,err=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,tenantID.String());err!=nil{t.Fatal(err)}
 if _,err=tx.Exec(ctx,`SET LOCAL ROLE photo_api`);err!=nil{t.Fatal(err)}
 reserve:=func(key string,body []byte)(string,*uuid.UUID,*uuid.UUID){
  t.Helper();var outcome string;var upload,token *uuid.UUID;var state,multipart *string
  if err:=tx.QueryRow(ctx,`SELECT outcome,upload_id,token,state,multipart_id FROM photo.reserve_v30_archive_upload($1,$2,$3,$4,$5,'photos')`,tenantID,actor,session,key,string(body)).Scan(&outcome,&upload,&token,&state,&multipart);err!=nil{t.Fatal(err)}
  return outcome,upload,token
 }
 outcome,upload,token:=reserve("first-key",first.RequestBody());if outcome!="created"||upload==nil||*upload!=first.ID()||token==nil{t.Fatal("first reserve",outcome,upload,token)}
 if outcome,_,_:=reserve("first-key",first.RequestBody());outcome!="replayed"{t.Fatal("same key lost idempotency",outcome)}
 if outcome,_,_:=reserve("second-key",second.RequestBody());outcome!="reconcile"{t.Fatal("live reservation allowed another key",outcome)}
 var generation,writer uuid.UUID;var objectKey string
 wrongSHA:=rawSHA([]byte("different request"))
 err=tx.QueryRow(ctx,`SELECT generation_id,writer_token,object_key FROM photo.start_v30_archive_begin_generation_checked($1,$2,$3,$4,$5,$6)`,tenantID,actor,session,*upload,*token,wrongSHA).Scan(&generation,&writer,&objectKey)
 if !errors.Is(err,pgx.ErrNoRows){t.Fatal("mismatched request admitted generation",err)}
 if err:=tx.QueryRow(ctx,`SELECT generation_id,writer_token,object_key FROM photo.start_v30_archive_begin_generation_checked($1,$2,$3,$4,$5,$6)`,tenantID,actor,session,*upload,*token,first.RequestSHA()).Scan(&generation,&writer,&objectKey);err!=nil{t.Fatal(err)}
 if generation==uuid.Nil||writer==uuid.Nil||objectKey==""{t.Fatal("generation not started")}
 var readGeneration,readWriter uuid.UUID;var readKey,readState string;var readMultipart *string
 err=tx.QueryRow(ctx,`SELECT generation_id,writer_token,object_key,state,multipart_id FROM photo.read_v30_archive_begin_generation_checked($1,$2,$3,$4,$5,$6)`,tenantID,actor,session,*upload,*token,wrongSHA).Scan(&readGeneration,&readWriter,&readKey,&readState,&readMultipart)
 if !errors.Is(err,pgx.ErrNoRows){t.Fatal("mismatched request read generation",err)}
 if err:=tx.QueryRow(ctx,`SELECT generation_id,writer_token,object_key,state,multipart_id FROM photo.read_v30_archive_begin_generation_checked($1,$2,$3,$4,$5,$6)`,tenantID,actor,session,*upload,*token,first.RequestSHA()).Scan(&readGeneration,&readWriter,&readKey,&readState,&readMultipart);err!=nil||readGeneration!=generation||readWriter!=writer||readKey!=objectKey||readState!="creating"||readMultipart!=nil{t.Fatal("matching request lost generation",err)}
 freshSession:=uuid.New()
 if _,err=tx.Exec(ctx,`RESET ROLE`);err!=nil{t.Fatal(err)}
 if _,err=tx.Exec(ctx,`INSERT INTO photo.sessions(id,user_id,refresh_hash,family_id,client_type,expires_at) VALUES($1,$2,$3,$4,'native',now()+interval '1 hour')`,freshSession,actor,auth.HashSecret(uuid.NewString()),uuid.New());err!=nil{t.Fatal(err)}
 if _,err=tx.Exec(ctx,`SET LOCAL ROLE photo_api`);err!=nil{t.Fatal(err)}
 var statusState string;var statusMultipart *string
 if err=tx.QueryRow(ctx,`SELECT state,multipart_id FROM photo.v30_archive_begin_generation_status($1,$2,$3,$4,$5,$6,$7)`,tenantID,project,actor,freshSession,*upload,*token,first.RequestSHA()).Scan(&statusState,&statusMultipart);err!=nil||statusState!="creating"||statusMultipart!=nil{t.Fatal("new authorized session lost read-only Begin state",err)}
 err=tx.QueryRow(ctx,`SELECT generation_id,writer_token,object_key,state,multipart_id FROM photo.read_v30_archive_begin_generation_checked($1,$2,$3,$4,$5,$6)`,tenantID,actor,freshSession,*upload,*token,first.RequestSHA()).Scan(&readGeneration,&readWriter,&readKey,&readState,&readMultipart)
 if !errors.Is(err,pgx.ErrNoRows){t.Fatal("new session obtained creator-bound Begin ticket",err)}
 err=tx.QueryRow(ctx,`SELECT state,multipart_id FROM photo.v30_archive_begin_generation_status($1,$2,$3,$4,$5,$6,$7)`,tenantID,project,actor,freshSession,*upload,*token,wrongSHA).Scan(&statusState,&statusMultipart)
 if !errors.Is(err,pgx.ErrNoRows){t.Fatal("new session read changed request state",err)}
 if outcome,_,_:=reserve("second-key",second.RequestBody());outcome!="reconcile"{t.Fatal("open Create allowed another key",outcome)}
 var closed bool
 if err:=tx.QueryRow(ctx,`SELECT photo.finish_v30_archive_begin_not_dispatched($1,$2,$3,$4,$5,$6,$7)`,tenantID,actor,session,*upload,*token,generation,writer).Scan(&closed);err!=nil||!closed{t.Fatal("definite no-dispatch close",closed,err)}
 var retryGeneration,retryWriter uuid.UUID;var retryKey string
 if err:=tx.QueryRow(ctx,`SELECT generation_id,writer_token,object_key FROM photo.start_v30_archive_begin_generation_checked($1,$2,$3,$4,$5,$6)`,tenantID,actor,session,*upload,*token,first.RequestSHA()).Scan(&retryGeneration,&retryWriter,&retryKey);err!=nil{t.Fatal("same reservation could not retry after proven no-dispatch",err)}
 if retryGeneration==uuid.Nil||retryWriter==uuid.Nil||retryGeneration==generation||retryWriter==writer||retryKey==objectKey{t.Fatal("retry reused the previous Create identity")}
 if err:=tx.QueryRow(ctx,`SELECT photo.finish_v30_archive_begin_not_dispatched($1,$2,$3,$4,$5,$6,$7)`,tenantID,actor,session,*upload,*token,retryGeneration,retryWriter).Scan(&closed);err!=nil||!closed{t.Fatal("retried no-dispatch close",closed,err)}
 if outcome,_,_:=reserve("second-key",second.RequestBody());outcome!="reconcile"{t.Fatal("live parent reservation allowed another key",outcome)}
 if _,err=tx.Exec(ctx,`RESET ROLE`);err!=nil{t.Fatal(err)}
 if _,err=tx.Exec(ctx,`UPDATE photo.v30_archive_uploads SET expires_at=clock_timestamp()-interval '1 minute' WHERE tenant_id=$1 AND id=$2`,tenantID,*upload);err!=nil{t.Fatal(err)}
 if _,err=tx.Exec(ctx,`SET LOCAL ROLE photo_api`);err!=nil{t.Fatal(err)}
 outcome,other,secondToken:=reserve("second-key",second.RequestBody());if outcome!="created"||other==nil||*other!=second.ID()||secondToken==nil{t.Fatal("terminal expired predecessor still blocked distinct key",outcome,other)}
 // Simulate a duplicate reservation committed before 01805. The newer row
 // cannot enter either Start path while the older live row is waiting.
 thirdRequest:=request;thirdRequest.Filename="third.zip"
 third,err:=FreezeArchiveUpload(ctx,tenantID,who,"third-key",thirdRequest);if err!=nil{t.Fatal(err)}
 thirdToken:=uuid.New()
 if _,err=tx.Exec(ctx,`RESET ROLE`);err!=nil{t.Fatal(err)}
 if _,err=tx.Exec(ctx,`INSERT INTO photo.v30_archive_uploads(tenant_id,id,project_id,actor_id,key,request_body,request_sha,filename,expected_bytes,expected_sha,artifact_id,bucket,object_key,token,authority_session_id) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,'photos',$12,$13,$14)`,tenantID,third.ID(),project,actor,"third-key",string(third.RequestBody()),third.RequestSHA(),thirdRequest.Filename,thirdRequest.Bytes,thirdRequest.SHA256,third.ArtifactID(),third.ObjectKey(),thirdToken,session);err!=nil{t.Fatal(err)}
 if _,err=tx.Exec(ctx,`SET LOCAL ROLE photo_api`);err!=nil{t.Fatal(err)}
 var deniedGeneration,deniedWriter uuid.UUID;var deniedKey string
 err=tx.QueryRow(ctx,`SELECT generation_id,writer_token,object_key FROM photo.start_v30_archive_begin_generation_checked($1,$2,$3,$4,$5,$6)`,tenantID,actor,session,third.ID(),thirdToken,third.RequestSHA()).Scan(&deniedGeneration,&deniedWriter,&deniedKey)
 if !errors.Is(err,pgx.ErrNoRows){t.Fatal("newer preexisting reservation entered generation Start",err)}
 var required bool
 if err=tx.QueryRow(ctx,`SELECT photo.v30_archive_cross_key_reconciliation_required($1,$2,$3,$4,$5)`,tenantID,actor,session,third.ID(),thirdToken).Scan(&required);err!=nil||!required{t.Fatal("cross-key refusal not classified",required,err)}
 var secondGeneration,secondWriter uuid.UUID;var secondKey string
 if err=tx.QueryRow(ctx,`SELECT generation_id,writer_token,object_key FROM photo.start_v30_archive_begin_generation_checked($1,$2,$3,$4,$5,$6)`,tenantID,actor,session,*other,*secondToken,second.RequestSHA()).Scan(&secondGeneration,&secondWriter,&secondKey);err!=nil{t.Fatal("older reservation refused",err)}
 if secondGeneration==uuid.Nil||secondWriter==uuid.Nil||secondKey==""{t.Fatal("older generation missing")}
 if err=tx.QueryRow(ctx,`SELECT photo.v30_archive_cross_key_reconciliation_required($1,$2,$3,$4,$5)`,tenantID,actor,session,*other,*secondToken).Scan(&required);err!=nil||required{t.Fatal("winner classified as cross-key conflict",required,err)}
 err=tx.QueryRow(ctx,`SELECT generation_id,writer_token,object_key FROM photo.start_v30_archive_begin_generation_checked($1,$2,$3,$4,$5,$6)`,tenantID,actor,session,third.ID(),thirdToken,third.RequestSHA()).Scan(&deniedGeneration,&deniedWriter,&deniedKey)
 if !errors.Is(err,pgx.ErrNoRows){t.Fatal("open sibling generation admitted newer Start",err)}
 var legacyWriter *uuid.UUID
 if err=tx.QueryRow(ctx,`SELECT photo.start_v30_archive_begin($1,$2,$3,$4,$5)`,tenantID,actor,session,third.ID(),thirdToken).Scan(&legacyWriter);err!=nil||legacyWriter!=nil{t.Fatal("legacy Start bypassed open sibling",legacyWriter,err)}
}

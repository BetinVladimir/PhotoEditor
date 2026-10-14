package interchange
import (
 "context"
 "errors"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
// Revocation is separate from purge: only positive physical session revocation
// can release an open Complete ticket. It never releases quota or publishes.
type ArchiveCompleteRevocation struct{DB artifactjson.RowQuerier;Objects ArchiveMultipartDrain;Bucket string}
func(s ArchiveCompleteRevocation)ProcessOne(ctx context.Context)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if s.DB==nil||s.Objects==nil||len(s.Bucket)<3||len(s.Bucket)>63{return false,v2input.ErrInvalid}
 var tid,upload,cleanup,writer uuid.UUID;var bucket,key,multipart string
 claim,stop:=context.WithTimeout(ctx,5*time.Second)
 err:=s.DB.QueryRow(claim,`SELECT tenant_id,upload_id,cleanup_token,writer_token,bucket,object_key,multipart_id FROM photo.claim_v30_archive_complete_revocation()`).Scan(&tid,&upload,&cleanup,&writer,&bucket,&key,&multipart);stop()
 if errors.Is(err,pgx.ErrNoRows){return false,nil};if err!=nil{return false,err}
 expected,err:=objectstore.InterchangeUploadKey(tid,upload)
 if err!=nil||cleanup==uuid.Nil||writer==uuid.Nil||bucket!=s.Bucket||key!=expected||!objectstore.ValidInterchangeMultipartID(multipart){return false,objectstore.ErrIntegrity}
 // Work is bounded far below the two-minute lease; no lease extension can turn
 // a stale physical observation into authority. Finish rechecks every pin.
 work,done:=context.WithTimeout(ctx,30*time.Second);defer done()
 if err:=s.Objects.DrainInterchangeMultipart(work,tid,upload,multipart);err!=nil{return false,err}
 if err:=work.Err();err!=nil{return false,err}
 var accepted bool
 finish,finishDone:=context.WithTimeout(work,5*time.Second);defer finishDone()
 if err:=s.DB.QueryRow(finish,`SELECT photo.finish_v30_archive_complete_revocation($1,$2,$3,$4,$5,$6,$7)`,tid,upload,cleanup,writer,bucket,key,multipart).Scan(&accepted);err!=nil{return false,err}
 if !accepted{return false,ErrArchiveCleanupLeaseLost};return true,finish.Err()
}

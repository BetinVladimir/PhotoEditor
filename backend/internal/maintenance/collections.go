package maintenance

import (
 "context"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
)
// CollectionCleaner owns only the logical catalogue withdrawal. It never
// obtains object-store deletion authority or removes immutable GC roots.
type CollectionCleaner struct{ DB artifactjson.RowQuerier }
func(c CollectionCleaner)CleanOne(ctx context.Context)(bool,error){
 if c.DB==nil{return false,errors.New("collection cleanup database required")}
 var job,token,tenant,collection uuid.UUID
 err:=c.DB.QueryRow(ctx,`SELECT job_id,cleanup_token,tenant_id,collection_id FROM photo.claim_collection_deletion()`).Scan(&job,&token,&tenant,&collection)
 if errors.Is(err,pgx.ErrNoRows){return false,nil};if err!=nil{return false,err}
 if job==uuid.Nil||token==uuid.Nil||tenant==uuid.Nil||collection==uuid.Nil{return false,errors.New("invalid collection cleanup claim")}
 if err=ctx.Err();err!=nil{return false,err};var finished bool
 err=c.DB.QueryRow(ctx,`SELECT photo.finish_collection_deletion($1,$2,$3,$4)`,tenant,job,collection,token).Scan(&finished)
 return finished,err
}

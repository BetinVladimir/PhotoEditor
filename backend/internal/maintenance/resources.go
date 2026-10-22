package maintenance

import (
 "context"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/artifactjson"
)

// ResourceWithdrawalCleaner settles archive/tombstone work. It has no storage
// deletion port and cannot purge retained or shared image evidence.
type ResourceWithdrawalCleaner struct{ DB artifactjson.RowQuerier }
func(c ResourceWithdrawalCleaner)CleanOne(ctx context.Context)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if c.DB==nil{return false,errors.New("resource withdrawal database required")}
 var job,token,tenant,resource uuid.UUID;var kind string
 err:=c.DB.QueryRow(ctx,`SELECT job_id,cleanup_token,tenant_id,resource_type,resource_id FROM photo.claim_resource_withdrawal()`).Scan(&job,&token,&tenant,&kind,&resource)
 if errors.Is(err,pgx.ErrNoRows){return false,nil};if err!=nil{return false,err}
 if job==uuid.Nil||token==uuid.Nil||tenant==uuid.Nil||resource==uuid.Nil||(kind!="project"&&kind!="asset"){return false,errors.New("invalid resource withdrawal claim")}
 if err=ctx.Err();err!=nil{return false,err};var finished bool
 err=c.DB.QueryRow(ctx,`SELECT photo.finish_resource_withdrawal($1,$2,$3,$4,$5)`,tenant,job,kind,resource,token).Scan(&finished)
 if err==nil{err=ctx.Err()};return finished,err
}

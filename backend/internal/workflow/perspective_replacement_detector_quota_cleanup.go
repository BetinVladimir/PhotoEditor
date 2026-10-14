package workflow

import (
 "context"
 "errors"
 "time"
 "github.com/jackc/pgx/v5"
)

type ReplacementDetectorQuotaCleanup struct {DB interface {QueryRow(context.Context,string,...any)pgx.Row}}
func(p ReplacementDetectorQuotaCleanup)ProcessOne(ctx context.Context)(bool,error){
 if err:=ctx.Err();err!=nil {return false,err}
 if p.DB==nil {return false,errors.New("replacement detector quota database required")}
 ctx,cancel:=context.WithTimeout(ctx,10*time.Second);defer cancel()
 var changed bool
 if err:=p.DB.QueryRow(ctx,"SELECT photo.release_next_v29_replacement_detector_quota()").Scan(&changed);err!=nil {return false,err}
 return changed,nil
}

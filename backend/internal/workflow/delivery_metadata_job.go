package workflow

import (
 "context"
 "errors"
 "time"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)
type DeliveryMetadataLeases interface {
 Next(context.Context)(*delivery.MetadataBarrierLease,error)
 Release(context.Context,delivery.MetadataBarrierLease)error
}
type DeliveryMetadataMaterializer interface{StageClaimed(context.Context,delivery.MetadataBarrierLease)error}
type DeliveryMetadataJob struct{Leases DeliveryMetadataLeases;Stage DeliveryMetadataMaterializer}
func(j DeliveryMetadataJob)ProcessOne(ctx context.Context)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if j.Leases==nil||j.Stage==nil{return false,v2input.ErrInvalid}
 bounded,cancel:=context.WithTimeout(ctx,45*time.Second);defer cancel()
 lease,err:=j.Leases.Next(bounded);if err!=nil||lease==nil{return false,err}
 if err=j.Stage.StageClaimed(bounded,*lease);err==nil{return true,nil}
 // Best-effort token-fenced release even when the processing context timed out.
 // If activation committed with an unknown outcome, release cannot reopen it.
 cleanup,stop:=context.WithTimeout(context.WithoutCancel(ctx),5*time.Second);defer stop()
 return false,errors.Join(err,j.Leases.Release(cleanup,*lease))
}

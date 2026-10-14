package workflow

import (
 "context"
 "errors"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/delivery"
)
type metadataJobLeases struct{lease *delivery.MetadataBarrierLease;err,releaseErr error;claims,releases int}
func(s *metadataJobLeases)Next(ctx context.Context)(*delivery.MetadataBarrierLease,error){
 s.claims++;deadline,ok:=ctx.Deadline();if !ok||time.Until(deadline)>45*time.Second{panic("unbounded metadata job")};return s.lease,s.err
}
func(s *metadataJobLeases)Release(ctx context.Context,lease delivery.MetadataBarrierLease)error{
 s.releases++;if ctx.Err()!=nil||lease!=*s.lease{panic("cleanup lost token or inherited cancellation")};return s.releaseErr
}
type metadataJobStage struct{err error;calls int;cancel context.CancelFunc}
func(s *metadataJobStage)StageClaimed(_ context.Context,_ delivery.MetadataBarrierLease)error{s.calls++;if s.cancel!=nil{s.cancel()};return s.err}
func TestMetadataJobClaimStageAndFencedCleanup(t *testing.T){
 failure:=errors.New("stage failed");releaseFailure:=errors.New("release failed")
 for _,mode:=range []string{"idle","claim_error","success","stage_error","cleanup_error","cancelled_stage"}{
  t.Run(mode,func(t *testing.T){
   ctx,cancel:=context.WithCancel(context.Background());defer cancel()
   leases:=&metadataJobLeases{lease:&delivery.MetadataBarrierLease{Tenant:uuid.New(),ChildRun:uuid.New(),Token:uuid.New()}}
   stage:=&metadataJobStage{}
   switch mode{case "idle":leases.lease=nil;case "claim_error":leases.err=failure;case "stage_error":stage.err=failure;case "cleanup_error":stage.err=failure;leases.releaseErr=releaseFailure;case "cancelled_stage":stage.err=context.Canceled;stage.cancel=cancel}
   progress,err:=(DeliveryMetadataJob{Leases:leases,Stage:stage}).ProcessOne(ctx)
   if mode=="success"{if err!=nil||!progress||stage.calls!=1||leases.releases!=0{t.Fatal("success",err)};return}
   if mode=="idle"{if err!=nil||progress||stage.calls!=0{t.Fatal("idle",err)};return}
   if err==nil||progress{t.Fatal("failure swallowed")}
   if mode=="claim_error"{if stage.calls!=0||leases.releases!=0{t.Fatal("claim error materialized")};return}
   if leases.releases!=1{t.Fatal("lease not released")}
   if mode=="cleanup_error"&&(!errors.Is(err,failure)||!errors.Is(err,releaseFailure)){t.Fatal("lost cleanup error")}
  })
 }
 leases:=&metadataJobLeases{};stage:=&metadataJobStage{};ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=(DeliveryMetadataJob{Leases:leases,Stage:stage}).ProcessOne(ctx);err!=context.Canceled||leases.claims!=0{t.Fatal("cancel reached claim")}
}

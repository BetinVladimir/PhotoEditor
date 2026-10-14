package interchange

import (
 "context"
 "errors"
 "testing"
 "time"

 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
)

type generationCleanupStub struct{
 lease ArchiveGenerationCleanupLease
 renew,authorize,release,drain,purge,finish int
 deny bool
 failAt string
 cancel context.CancelFunc
 cancelAtFinish context.CancelFunc
 cancelOnClaim context.CancelFunc
 claimErr error
}
func(s *generationCleanupStub)Claim(context.Context)(*ArchiveGenerationCleanupLease,error){if s.cancelOnClaim!=nil{s.cancelOnClaim()};return &s.lease,s.claimErr}
func(s *generationCleanupStub)Renew(context.Context,ArchiveGenerationCleanupLease)(bool,error){s.renew++;return !s.deny,nil}
func(s *generationCleanupStub)Authorize(context.Context,ArchiveGenerationCleanupLease)(bool,error){s.authorize++;if s.failAt=="authorize"{return false,nil};return true,nil}
func(s *generationCleanupStub)Release(context.Context,ArchiveGenerationCleanupLease)(bool,error){s.release++;return true,nil}
func(s *generationCleanupStub)Finish(context.Context,ArchiveGenerationCleanupLease)(bool,error){s.finish++;if s.cancelAtFinish!=nil{s.cancelAtFinish()};return s.failAt!="finish",nil}
func(s *generationCleanupStub)DrainInterchangeGenerationMultipart(_ context.Context,tenant,upload,generation uuid.UUID,multipart string)error{
 s.drain++;if tenant!=s.lease.Tenant||upload!=s.lease.Upload||generation!=s.lease.Generation||multipart!=s.lease.MultipartID{return objectstore.ErrIntegrity}
 if s.failAt=="drain"{return objectstore.ErrUnavailable};if s.cancel!=nil{s.cancel()};return nil
}
func(s *generationCleanupStub)PurgeInterchangeGeneration(_ context.Context,tenant,upload,generation uuid.UUID)error{
 s.purge++;if tenant!=s.lease.Tenant||upload!=s.lease.Upload||generation!=s.lease.Generation{return objectstore.ErrIntegrity}
 if s.failAt=="purge"{return objectstore.ErrUnavailable};return nil
}
func newGenerationCleanupStub()*generationCleanupStub{
 s:=&generationCleanupStub{lease:ArchiveGenerationCleanupLease{Tenant:uuid.New(),Upload:uuid.New(),Generation:uuid.New(),Token:uuid.New(),Bucket:"photos",MultipartID:"multipart",Until:time.Now().Add(time.Minute)}}
 s.lease.Key,_=objectstore.InterchangeGenerationKey(s.lease.Tenant,s.lease.Upload,s.lease.Generation)
 return s
}
func TestGenerationCleanupRequiresRepeatedAuthority(t *testing.T){
 for _,mode:=range []string{"ok","lost","authorize","drain","purge","finish","bad-key"}{t.Run(mode,func(t *testing.T){
  s:=newGenerationCleanupStub();s.failAt=mode;if mode=="lost"{s.deny=true};if mode=="bad-key"{s.lease.Key+=".other"}
  ok,err:=(ArchiveGenerationCleanupProcessor{Leases:s,Objects:s}).ProcessOne(context.Background())
  if mode=="ok"{if !ok||err!=nil||s.drain!=1||s.purge!=1||s.authorize!=3||s.finish!=1||s.release!=0{t.Fatal("generation cleanup failed",ok,err,s)};return}
  if ok||err==nil||s.release!=1{t.Fatal("unsafe cleanup success",mode,ok,err,s)}
  if (mode=="lost"||mode=="authorize"||mode=="bad-key")&&(s.drain!=0||s.purge!=0){t.Fatal("unauthorized generation IO",mode,s)}
  if mode=="drain"&&s.purge!=0{t.Fatal("purged after failed drain")}
  if mode=="purge"&&s.finish!=0{t.Fatal("finished after failed purge")}
 })}
}
func TestGenerationCleanupCancellationAfterDrainStopsPurge(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());defer cancel()
 s:=newGenerationCleanupStub();s.cancel=cancel
 ok,err:=(ArchiveGenerationCleanupProcessor{Leases:s,Objects:s}).ProcessOne(ctx)
 if ok||!errors.Is(err,context.Canceled)||s.drain!=1||s.purge!=0||s.finish!=0||s.release!=1{t.Fatal("cancelled cleanup advanced",ok,err,s)}
}
func TestGenerationCleanupRefusesSuccessAfterLateFinishCancellation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());defer cancel()
 s:=newGenerationCleanupStub();s.cancelAtFinish=cancel
 ok,err:=(ArchiveGenerationCleanupProcessor{Leases:s,Objects:s}).ProcessOne(ctx)
 if ok||!errors.Is(err,context.Canceled)||s.finish!=1||s.release!=1||s.drain!=1||s.purge!=1{t.Fatal("late cancellation reported completed generation cleanup",ok,err,s)}
}
func TestGenerationCleanupReleasesLeaseReturnedWithClaimCancellation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());defer cancel()
 s:=newGenerationCleanupStub();s.cancelOnClaim=cancel;s.claimErr=context.Canceled
 ok,err:=(ArchiveGenerationCleanupProcessor{Leases:s,Objects:s}).ProcessOne(ctx)
 if ok||!errors.Is(err,context.Canceled)||s.release!=1||s.renew!=0||s.authorize!=0||s.drain!=0||s.purge!=0||s.finish!=0{t.Fatal("generation claim lease leaked",ok,err,s)}
}

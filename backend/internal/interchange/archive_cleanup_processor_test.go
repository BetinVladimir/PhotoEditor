package interchange

import (
 "context"
 "errors"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
)
type cleanupProcessorStub struct{
 lease ArchiveCleanupLease
 renew,release,finish,purge,guard int
 lose,refuse bool
 purgeErr,guardErr,secondGuardErr error
 panicOnPurge bool
 cancelAfterFirstGuard context.CancelFunc
 cancelAtFinish context.CancelFunc
 cancelOnClaim context.CancelFunc
 claimErr error
}
func(s *cleanupProcessorStub)Claim(context.Context)(*ArchiveCleanupLease,error){if s.cancelOnClaim!=nil{s.cancelOnClaim()};return &s.lease,s.claimErr}
func(s *cleanupProcessorStub)Renew(context.Context,ArchiveCleanupLease)(bool,error){s.renew++;return !s.lose,nil}
func(s *cleanupProcessorStub)Release(context.Context,ArchiveCleanupLease)(bool,error){s.release++;return true,nil}
func(s *cleanupProcessorStub)Finish(context.Context,ArchiveCleanupLease)(bool,error){s.finish++;if s.cancelAtFinish!=nil{s.cancelAtFinish()};return !s.refuse,nil}
func(s *cleanupProcessorStub)PurgeInterchangeArchiveKey(_ context.Context,tenant,upload uuid.UUID,key string)error{s.purge++;if s.panicOnPurge{panic("object-store panic")};if tenant!=s.lease.Tenant||upload!=s.lease.Upload||key!=s.lease.Key{return errors.New("wrong purge scope")};return s.purgeErr}
func(s *cleanupProcessorStub)AssertQuiescent(context.Context,ArchiveCleanupLease)error{s.guard++;if s.guard==1&&s.cancelAfterFirstGuard!=nil{s.cancelAfterFirstGuard()};if s.guard==2&&s.secondGuardErr!=nil{return s.secondGuardErr};return s.guardErr}
func newCleanupProcessorStub()*cleanupProcessorStub{
 s:=&cleanupProcessorStub{lease:ArchiveCleanupLease{Tenant:uuid.New(),Upload:uuid.New(),Token:uuid.New(),Bucket:"photos",Until:time.Now().Add(time.Minute),NotBefore:time.Now().Add(-time.Minute)}}
 s.lease.Key,_=objectstore.InterchangeUploadKey(s.lease.Tenant,s.lease.Upload);return s
}
func TestArchiveCleanupProcessorCommitAndFailures(t *testing.T){
 for _,mode:=range []string{"ok","lease-lost","writers-active","purge-failure","finish-refused","invalid-key"}{t.Run(mode,func(t *testing.T){
  s:=newCleanupProcessorStub();switch mode{case "lease-lost":s.lose=true;case "writers-active":s.guardErr=errors.New("writer active");case "purge-failure":s.purgeErr=errors.New("S3 unavailable");case "finish-refused":s.refuse=true;case "invalid-key":s.lease.Key+=".other"}
  ok,err:=(ArchiveCleanupProcessor{Leases:s,Objects:s,Writers:s}).ProcessOne(context.Background())
  if mode=="ok"{if !ok||err!=nil||s.finish!=1||s.release!=0||s.guard!=2||s.purge!=1{t.Fatal("invalid success",ok,err,s)};return}
  if ok||err==nil||s.release!=1{t.Fatal("false success or lease leak",ok,err,s)}
  if mode!="finish-refused"&&s.finish!=0{t.Fatal("quota released on failure")}
  if (mode=="lease-lost"||mode=="writers-active"||mode=="invalid-key")&&s.purge!=0{t.Fatal("purged without authority")}
 })}
}
func TestArchiveCleanupProcessorRequiresWriterGuard(t *testing.T){
 s:=newCleanupProcessorStub();if _,err:=(ArchiveCleanupProcessor{Leases:s,Objects:s}).ProcessOne(context.Background());err==nil{t.Fatal("missing writer guard accepted")}
 if s.renew!=0||s.purge!=0||s.finish!=0{t.Fatal("IO without guard")}
}

func TestArchiveCleanupRefusesFinishWhenWriterReappearsAfterPurge(t *testing.T){
 s:=newCleanupProcessorStub();s.secondGuardErr=errors.New("writer became active")
 ok,err:=(ArchiveCleanupProcessor{Leases:s,Objects:s,Writers:s}).ProcessOne(context.Background())
 if ok||!errors.Is(err,s.secondGuardErr)||s.guard!=2||s.purge!=1||s.finish!=0||s.release!=1{
  t.Fatal("cleanup acknowledged while a writer was active",ok,err,s)
 }
}

func TestArchiveCleanupProcessorPanicStopsHeartbeatBeforeRelease(t *testing.T){
 s:=newCleanupProcessorStub();s.panicOnPurge=true
 defer func(){
  if recovered:=recover();recovered==nil{t.Fatal("adapter panic swallowed")}
  if s.release!=1||s.finish!=0||s.guard!=1||s.purge!=1{t.Fatal("cleanup lease or authority advanced after panic",s)}
 }()
 _,_=(ArchiveCleanupProcessor{Leases:s,Objects:s,Writers:s,HeartbeatInterval:time.Millisecond}).ProcessOne(context.Background())
}

func TestArchiveCleanupRefusesPurgeAfterGuardCancellation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());defer cancel()
 s:=newCleanupProcessorStub();s.cancelAfterFirstGuard=cancel
 ok,err:=(ArchiveCleanupProcessor{Leases:s,Objects:s,Writers:s}).ProcessOne(ctx)
 if ok||!errors.Is(err,context.Canceled)||s.guard!=1||s.purge!=0||s.finish!=0||s.release!=1{
  t.Fatal("cancelled guard reached destructive purge",ok,err,s)
 }
}
func TestArchiveCleanupRefusesSuccessAfterLateFinishCancellation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());defer cancel()
 s:=newCleanupProcessorStub();s.cancelAtFinish=cancel
 ok,err:=(ArchiveCleanupProcessor{Leases:s,Objects:s,Writers:s}).ProcessOne(ctx)
 if ok||!errors.Is(err,context.Canceled)||s.finish!=1||s.release!=1||s.purge!=1{t.Fatal("late cancellation reported completed cleanup",ok,err,s)}
}
func TestArchiveCleanupReleasesLeaseReturnedWithClaimCancellation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());defer cancel()
 s:=newCleanupProcessorStub();s.cancelOnClaim=cancel;s.claimErr=context.Canceled
 ok,err:=(ArchiveCleanupProcessor{Leases:s,Objects:s,Writers:s}).ProcessOne(ctx)
 if ok||!errors.Is(err,context.Canceled)||s.release!=1||s.renew!=0||s.guard!=0||s.purge!=0||s.finish!=0{t.Fatal("claimed lease leaked after cancellation",ok,err,s)}
}
func TestArchiveParentCleanupAfterGenerationPurgeUsesSameKey(t *testing.T){
 s:=newCleanupProcessorStub();generation:=uuid.New();s.lease.Key,_=objectstore.InterchangeGenerationKey(s.lease.Tenant,s.lease.Upload,generation)
 ok,err:=(ArchiveCleanupProcessor{Leases:s,Objects:s,Writers:s}).ProcessOne(context.Background())
 if err!=nil||!ok||s.purge!=1||s.guard!=2||s.finish!=1{t.Fatal("generation parent cleanup did not finish",ok,err,s)}
}

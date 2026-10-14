package main
import (
 "context"
 "errors"
 "testing"
)
type cleanupSequenceJob struct{calls int;done bool;err error}
func(j *cleanupSequenceJob)ProcessOne(context.Context)(bool,error){j.calls++;return j.done,j.err}
func TestArchiveCleanupSequenceRunsGenerationBeforeParent(t *testing.T){
 for _,mode:=range []string{"generation","parent","error"}{t.Run(mode,func(t *testing.T){
  gen,parent:=&cleanupSequenceJob{},&cleanupSequenceJob{done:true}
  if mode=="generation"{gen.done=true};if mode=="error"{gen.err=errors.New("generation cleanup failed")}
  done,err:=(archiveCleanupSequence{Generations:gen,Legacy:parent}).ProcessOne(context.Background())
  if gen.calls!=1||parent.calls!=map[string]int{"generation":0,"parent":1,"error":0}[mode]{t.Fatal("cleanup order changed",mode,gen,parent)}
  if mode=="error"{if !errors.Is(err,gen.err)||done{t.Fatal(err,done)}}else if err!=nil||!done{t.Fatal(err,done)}
 })}
}
func TestArchiveCompletionFactoryRequiresInstallation(t *testing.T){
 factory:=archiveCompletionFactory{}
 if _,err:=factory.BuildArchiveCompletion(context.Background(),nil,nil,"photos");err==nil{t.Fatal("installed without dependencies")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=factory.BuildArchiveCompletion(ctx,nil,nil,"photos");err!=context.Canceled{t.Fatal(err)}
}

func TestArchiveExpiryFactoryRequiresDatabaseAndHonorsCancellation(t *testing.T){
 factory:=archiveCompletionFactory{}
 if _,err:=factory.BuildArchiveExpiry(context.Background(),nil);err==nil{t.Fatal("expiry installed without database")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=factory.BuildArchiveExpiry(ctx,nil);err!=context.Canceled{t.Fatal(err)}
}

func TestArchiveCleanupFactoryRequiresInstalledDependencies(t *testing.T){
 factory:=archiveCompletionFactory{}
 if _,err:=factory.BuildArchiveCleanup(context.Background(),nil,nil,"photos");err==nil{t.Fatal("cleanup installed without dependencies")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=factory.BuildArchiveCleanup(ctx,nil,nil,"photos");err!=context.Canceled{t.Fatal(err)}
}

func TestArchiveRecoveryFactoryRequiresInstallation(t *testing.T){
 factory:=archiveCompletionFactory{}
 if _,err:=factory.BuildArchiveRecovery(context.Background(),nil,nil,"photos");err==nil{t.Fatal("recovery installed without dependencies")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=factory.BuildArchiveRecovery(ctx,nil,nil,"photos");err!=context.Canceled{t.Fatal(err)}
}

func TestArchiveRevocationFactoryRequiresInstallation(t *testing.T){
 factory:=archiveCompletionFactory{}
 if _,err:=factory.BuildArchiveRevocation(context.Background(),nil,nil,"photos");err==nil{t.Fatal("revocation installed without dependencies")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=factory.BuildArchiveRevocation(ctx,nil,nil,"photos");err!=context.Canceled{t.Fatal(err)}
}

func TestArchiveInvalidationFactoryRequiresDatabase(t *testing.T){
 factory:=archiveCompletionFactory{}
 if _,err:=factory.BuildArchiveInvalidation(context.Background(),nil);err==nil{t.Fatal("invalidation installed without database")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=factory.BuildArchiveInvalidation(ctx,nil);err!=context.Canceled{t.Fatal(err)}
}

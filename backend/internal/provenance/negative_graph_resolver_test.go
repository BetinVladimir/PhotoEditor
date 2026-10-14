package provenance

import (
 "context"
 "errors"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
)

type negativeCausalReaderFunc func(context.Context,auth.Identity,ReadySigningExport,CanonicalSigningTask)(signingNegativeCausalReceipt,error)
func(f negativeCausalReaderFunc)read(ctx context.Context,who auth.Identity,ready ReadySigningExport,task CanonicalSigningTask)(signingNegativeCausalReceipt,error){return f(ctx,who,ready,task)}

func TestNegativeResolverRequiresStableRepeatedAuthority(t *testing.T){
 for _,mode:=range []string{"stable","producer_changed","review_revoked","second_read_denied","cancelled"}{
  t.Run(mode,func(t *testing.T){
   task,receipt:=negativeGraphFixture(t)
   ctx,cancel:=context.WithCancel(context.Background());defer cancel()
   calls:=0;denied:=errors.New("authority revoked")
   reader:=negativeCausalReaderFunc(func(_ context.Context,_ auth.Identity,_ ReadySigningExport,received CanonicalSigningTask)(signingNegativeCausalReceipt,error){
    calls++
    // Adapters receive an owned task; mutation must not corrupt the caller.
    received.Metrics[0]='['
    current:=receipt
    if calls==1&&mode=="cancelled"{cancel()}
    if calls==2{
     switch mode{
     case "producer_changed":
      current.pixels.producer=uuid.New()
      var err error
      current.stamp,err=canonical.Hash([]any{current.evidence.stamp,current.pixels.input,current.pixels.producer,current.entries});if err!=nil{t.Fatal(err)}
     case "review_revoked":current.evidence.review.Accepted=false
     case "second_read_denied":return signingNegativeCausalReceipt{},denied
     }
    }
    return current,nil
   })
   who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
   ready:=ReadySigningExport{Tenant:uuid.New(),Project:uuid.New(),ExportID:uuid.New()}
   got,err:=(NegativeSigningTaskResolver{Causal:reader}).ResolveSigningTaskLineage(ctx,who,ready,task)
   if string(task.Metrics)!="{}"{t.Fatal("adapter mutated original task")}
   if mode=="stable"{
    if err!=nil||calls!=2||got.Node.Task!=task.Task||len(got.Node.Parents)!=1||got.Node.Parents[0]!=receipt.pixels.producer{t.Fatal("stable evidence not compiled",calls,err)}
   }else{
    if err==nil||got.Node.Task!=uuid.Nil{t.Fatal("unstable authority produced signing graph")}
    if mode=="cancelled"&&(calls!=1||!errors.Is(err,context.Canceled)){t.Fatal("cancelled request read authority again",calls,err)}
    if mode=="second_read_denied"&&!errors.Is(err,denied){t.Fatal("authority denial hidden",err)}
   }
  })
 }
}

func TestNegativeResolverRejectsOperationBeforeAuthorityIO(t *testing.T){
 for _,mode:=range []string{"operation","version"}{
  t.Run(mode,func(t *testing.T){
   task,_:=negativeGraphFixture(t)
   if mode=="operation"{task.Operation="render.compose"}else{task.Version="2"}
   calls:=0
   reader:=negativeCausalReaderFunc(func(context.Context,auth.Identity,ReadySigningExport,CanonicalSigningTask)(signingNegativeCausalReceipt,error){calls++;return signingNegativeCausalReceipt{},nil})
   _,err:=(NegativeSigningTaskResolver{Causal:reader}).ResolveSigningTaskLineage(context.Background(),auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},ReadySigningExport{Tenant:uuid.New(),Project:uuid.New(),ExportID:uuid.New()},task)
   if err==nil||calls!=0{t.Fatal("unsupported operation reached authority",calls,err)}
  })
 }
}

package provenance

import (
 "context"
 "errors"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func TestNegativePixelAncestryRequiresStableExactSourceProducer(t *testing.T){
 for _,mode:=range []string{"valid","self","missing","changed","error","cancelled","wrong_task","bad_source"}{
  t.Run(mode,func(t *testing.T){
   task:=CanonicalSigningTask{Task:uuid.New(),Operation:"optical.negative",Version:"1"}
   sha,err:=canonical.Hash(task);if err!=nil{t.Fatal(err)}
   source:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}
   evidence:=SigningNegativeEvidence{taskDigest:sha,pixels:SigningNegativePixelEvidence{taskDigest:sha}}
   evidence.pixels.params.Base.Source=source
   if mode=="bad_source"{evidence.pixels.params.Base.Source.SHA256="bad"};if mode=="wrong_task"{task.Task=uuid.New()}
   producer:=uuid.New();calls:=0;failure:=errors.New("producer lookup failed")
   resolver:=signingProducerFunc(func(_ context.Context,_ auth.Identity,_ ReadySigningExport,input v2input.Ref)(uuid.UUID,error){
    calls++;if input!=source{t.Fatal("wrong source resolved")}
    switch mode{case "self":return task.Task,nil;case "missing":return uuid.Nil,nil;case "changed":if calls>1{return uuid.New(),nil};case "error":return uuid.Nil,failure}
    return producer,nil
   })
   ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
   result,err:=ResolveSigningNegativePixelAncestry(ctx,resolver,auth.Identity{},ReadySigningExport{},task,evidence)
   if mode=="valid"{if err!=nil||calls!=2||result.input!=source||result.producer!=producer{t.Fatal("valid source edge refused",err)};return}
   if err==nil{t.Fatal("invalid producer accepted",mode)}
   if mode=="error"&&!errors.Is(err,failure){t.Fatal("authority error lost",err)}
   if (mode=="cancelled"||mode=="wrong_task"||mode=="bad_source")&&calls!=0{t.Fatal("invalid preflight performed IO")}
  })
 }
}

func TestNegativeCausalSourceRequiresConfiguredAuthorities(t *testing.T){
 if _,err:=(BoundSigningNegativeCausalSource{}).read(context.Background(),auth.Identity{},ReadySigningExport{},CanonicalSigningTask{});err==nil{t.Fatal("unconfigured causal source accepted")}
}

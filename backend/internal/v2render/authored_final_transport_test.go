package v2render

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "errors"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type authoredFinalPublisherFunc func(context.Context,uuid.UUID,int,string,[]byte)(v2input.Ref,error)
func(f authoredFinalPublisherFunc)Publish(c context.Context,t uuid.UUID,o int,k string,b []byte)(v2input.Ref,error){return f(c,t,o,k,b)}
func TestAuthoredFinalTransportOwnsInventoryAndRealDigest(t *testing.T){
 evidence,ref,profile:=authoredFinalTaskCompilerFixture(t);raw,_:=canonical.Marshal(profile)
 plan,err:=compileAuthoredFinalTask(evidence,ref,raw);if err!=nil{t.Fatal(err)}
 calls:=0;pub:=authoredFinalPublisherFunc(func(ctx context.Context,task uuid.UUID,ordinal int,contract string,body []byte)(v2input.Ref,error){
  calls++;if task!=plan.Task.ID||(contract!="InputClosureChunk"&&contract!="InputClosureManifest"){t.Fatal("transport scope")}
  sum:=sha256.Sum256(body);return v2input.Ref{ArtifactID:uuid.New(),SHA256:hex.EncodeToString(sum[:])},nil
 })
 graph,err:=PublishAuthoredFinalTransport(context.Background(),plan,pub)
 if err!=nil||calls!=2||graph.Built.Count!=len(plan.Entries)||len(graph.Direct)!=1||graph.Direct[0]!=ref{t.Fatal("actual transport",err)}
 expected:=plan.Entries[0];plan.Entries[0].Contract="Artifact"
 cursor,err:=graph.Open(context.Background());if err!=nil{t.Fatal(err)}
 entry,ok,err:=cursor.Next(context.Background());if err!=nil||!ok||entry!=expected{t.Fatal("graph borrowed caller slice")}
}
func TestAuthoredFinalTransportRefusesMutationBeforeIO(t *testing.T){
 evidence,ref,profile:=authoredFinalTaskCompilerFixture(t);raw,_:=canonical.Marshal(profile)
 plan,err:=compileAuthoredFinalTask(evidence,ref,raw);if err!=nil{t.Fatal(err)}
 plan.Task.ResourceClass="cpu"
 called:=false;pub:=authoredFinalPublisherFunc(func(context.Context,uuid.UUID,int,string,[]byte)(v2input.Ref,error){called=true;return v2input.Ref{},nil})
 if _,err=PublishAuthoredFinalTransport(context.Background(),plan,pub);err==nil||called{t.Fatal("modified plan reached IO")}
}
func TestAuthoredFinalTransportPreservesPublisherError(t *testing.T){
 evidence,ref,profile:=authoredFinalTaskCompilerFixture(t);raw,_:=canonical.Marshal(profile)
 plan,err:=compileAuthoredFinalTask(evidence,ref,raw);if err!=nil{t.Fatal(err)}
 expected:=errors.New("owned publisher failure")
 pub:=authoredFinalPublisherFunc(func(context.Context,uuid.UUID,int,string,[]byte)(v2input.Ref,error){return v2input.Ref{},expected})
 if _,err=PublishAuthoredFinalTransport(context.Background(),plan,pub);!errors.Is(err,expected){t.Fatal("publisher error replaced",err)}
}

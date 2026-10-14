package workflow

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type replacementTransportSink struct { calls int }
func(s *replacementTransportSink) Publish(ctx context.Context, task uuid.UUID, ordinal int, contract string, raw []byte)(v2input.Ref,error){
 s.calls++
 return v2input.Ref{ArtifactID:uuid.NewSHA1(task,[]byte(contract)),SHA256:renderDerivativeBytesSHA(raw)},nil
}
func TestReplacementDetectionTransportPacket(t *testing.T){
 descriptor,detector:=replacementDetectionDescriptorFixture(t)
 raw,err:=canonical.Marshal(descriptor);if err!=nil{t.Fatal(err)}
 plan:=PerspectiveReplacementDetectionPlan{detector:detector,descriptor:string(raw)}
 _,_,task,entries,err:=plan.PreparePublication(context.Background());if err!=nil{t.Fatal(err)}
 sink:=&replacementTransportSink{}
 closure,err:=plan.BuildInputClosure(context.Background(),task.Params,sink)
 if err!=nil{t.Fatal(err)}
 if sink.calls!=2 || closure.Built().Count!=len(entries) || len(closure.Documents())!=2{t.Fatal("incomplete transport")}
 wrong:=task.Params;wrong.ID=uuid.New()
 if _,err=plan.BuildInputClosure(context.Background(),wrong,sink);err!=v2input.ErrInvalid || sink.calls!=2{t.Fatal("foreign Params reached publication",err)}
 guarded:=replacementDetectionTransportPublisher{plan:detector,params:task.Params,out:sink}
 if _,err=guarded.Publish(context.Background(),task.ID,1,"InputClosureManifest",[]byte(`{}`));err!=v2input.ErrInvalid || sink.calls!=2{t.Fatal("manifest without chunk reached publication",err)}
 if _,err=guarded.Publish(context.Background(),task.ID,0,"InputClosureChunk",[]byte(`{}`));err!=v2input.ErrInvalid || sink.calls!=2{t.Fatal("altered chunk reached publication",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=plan.BuildInputClosure(ctx,task.Params,sink);err!=context.Canceled || sink.calls!=2{t.Fatal("cancelled transport reached publication",err)}
}

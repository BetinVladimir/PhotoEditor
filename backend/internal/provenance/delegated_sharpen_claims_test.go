package provenance

import (
 "context"
 "encoding/json"
 "os"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

func TestDelegatedSharpenClaimsBindDeltaAndParent(t *testing.T){
 fixture,err:=os.ReadFile("../review/testdata/working-images-python.json")
 if err!=nil{t.Fatal(err)}
 var images []json.RawMessage
 if json.Unmarshal(fixture,&images)!=nil||len(images)==0{t.Fatal("missing image")}
 for _,mode:=range []string{"valid","delta","changed_parent"}{
  t.Run(mode,func(t *testing.T){
   source,err:=review.ParseWorkingImage(images[0])
   if err!=nil{t.Fatal(err)}
   sourceRaw,sourceRef:=contractBytes(t,source)
   paramsRaw,paramsRef:=contractBytes(t,signingSharpenParams{
    Image:sourceRef,Radius:1,Strength:.5,Threshold:.01,MaxDelta:.03,
    Algorithm:"luma_unsharp_v1"})
   pixel:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("b",64)}
   after:=source
   after.Pixels=review.ArtifactRef{ID:pixel.ArtifactID,SHA256:pixel.SHA256}
   resultRaw,resultRef:=contractBytes(t,after)
   report:=signingDerivativeReport{Image:sourceRef,Operation:"sharpen",
    Applied:true,Width:source.Canvas.Width,Height:source.Canvas.Height,
    MaxDelta:.02,Warnings:[]string{}}
   if mode=="delta"{report.MaxDelta=.5}
   reportRaw,reportRef:=contractBytes(t,report)
   inputs,_:=canonical.Marshal([]v2input.Ref{sourceRef,
    {ArtifactID:source.Pixels.ID,SHA256:source.Pixels.SHA256}})
   outputs,_:=canonical.Marshal([]v2input.Ref{pixel,resultRef,reportRef})
   ready:=ReadySigningExport{Tenant:uuid.New(),Project:uuid.New(),
    ExportID:uuid.New(),Task:uuid.New(),Run:uuid.New()}
   task:=CanonicalSigningTask{Task:uuid.New(),Run:ready.Run,Result:uuid.New(),
    Attempt:1,Fence:1,Operation:"derivative.sharpen",Version:"1",
    Params:paramsRef,Inputs:inputs,Outputs:outputs,Metrics:json.RawMessage(`{}`),
    InputDigest:strings.Repeat("c",64),ResultDigest:strings.Repeat("d",64)}
   data:=map[v2input.Ref][]byte{sourceRef:sourceRaw,paramsRef:paramsRaw,
    resultRef:resultRaw,reportRef:reportRaw}
   docs:=delegatedDocumentsFunc(func(_ context.Context,_ CanonicalSigningTask,
    ref v2input.Ref,_ int)([]byte,error){
    raw,ok:=data[ref]
    if !ok{return nil,ErrInvalid}
    return append([]byte(nil),raw...),nil
   })
   calls:=0
   parent:=uuid.New()
   producer:=delegatedImageProducerFunc(func(_ context.Context,
    ref v2input.Ref)(uuid.UUID,error){
    calls++
    if ref!=sourceRef{return uuid.Nil,ErrInvalid}
    if mode=="changed_parent"&&calls>1{return uuid.New(),nil}
    return parent,nil
   })
   result,err:=(DelegatedSharpenSigningTaskResolver{Documents:docs,
    Producers:producer}).ResolveDelegatedTask(context.Background(),ready,task)
   if mode=="valid"{
    if err!=nil||len(result.Node.Parents)!=1||result.Node.Parents[0]!=parent||
     calls!=2{t.Fatal("delegated sharpen claim lost",err)}
   }else if err==nil{t.Fatal("invalid delegated sharpen accepted",mode)}
  })
 }
}

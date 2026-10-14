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

func TestDelegatedWatermarkClaimsKeepBothParents(t *testing.T){
 fixture,err:=os.ReadFile("../review/testdata/working-images-python.json")
 if err!=nil{t.Fatal(err)}
 var images []json.RawMessage
 if json.Unmarshal(fixture,&images)!=nil||len(images)==0{t.Fatal("missing image")}
 for _,mode:=range []string{"valid","missing_mark_parent"}{
  t.Run(mode,func(t *testing.T){
   source,err:=review.ParseWorkingImage(images[0])
   if err!=nil{t.Fatal(err)}
   sourceRaw,sourceRef:=contractBytes(t,source)
   mark:=source
   mark.Pixels=review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("b",64)}
   mark.Alpha=&review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("c",64)}
   mark.Canvas.Width=80
   mark.Canvas.Height=40
   markRaw,markRef:=contractBytes(t,mark)
   paramsRaw,paramsRef:=contractBytes(t,signingWatermarkParams{
    Image:sourceRef,Watermark:markRef,Opacity:.5,Anchor:"bottom_right",
    MarginX:.03,MarginY:.03,MaxWidth:.25,Algorithm:"linear_alpha_v1"})
   pixel:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("e",64)}
   after:=source
   after.Pixels=review.ArtifactRef{ID:pixel.ArtifactID,SHA256:pixel.SHA256}
   resultRaw,resultRef:=contractBytes(t,after)
   reportRaw,reportRef:=contractBytes(t,signingDerivativeReport{
    Image:sourceRef,Operation:"watermark",Applied:true,
    Width:source.Canvas.Width,Height:source.Canvas.Height,
    MaxDelta:.2,Warnings:[]string{}})
   inputs:=[]v2input.Ref{sourceRef,{ArtifactID:source.Pixels.ID,
    SHA256:source.Pixels.SHA256},markRef,{ArtifactID:mark.Pixels.ID,
    SHA256:mark.Pixels.SHA256},{ArtifactID:mark.Alpha.ID,
    SHA256:mark.Alpha.SHA256}}
   inputRaw,_:=canonical.Marshal(inputs)
   outputRaw,_:=canonical.Marshal([]v2input.Ref{pixel,resultRef,reportRef})
   ready:=ReadySigningExport{Tenant:uuid.New(),Project:uuid.New(),
    ExportID:uuid.New(),Task:uuid.New(),Run:uuid.New()}
   task:=CanonicalSigningTask{Task:uuid.New(),Run:ready.Run,Result:uuid.New(),
    Attempt:1,Fence:1,Operation:"derivative.watermark",Version:"1",
    Params:paramsRef,Inputs:inputRaw,Outputs:outputRaw,
    Metrics:json.RawMessage(`{}`),InputDigest:strings.Repeat("a",64),
    ResultDigest:strings.Repeat("d",64)}
   data:=map[v2input.Ref][]byte{sourceRef:sourceRaw,markRef:markRaw,
    paramsRef:paramsRaw,resultRef:resultRaw,reportRef:reportRaw}
   docs:=delegatedDocumentsFunc(func(_ context.Context,_ CanonicalSigningTask,
    ref v2input.Ref,_ int)([]byte,error){
    raw,ok:=data[ref]
    if !ok{return nil,ErrInvalid}
    return append([]byte(nil),raw...),nil
   })
   sourceParent,markParent:=uuid.New(),uuid.New()
   producer:=delegatedImageProducerFunc(func(_ context.Context,
    ref v2input.Ref)(uuid.UUID,error){
    if ref==sourceRef{return sourceParent,nil}
    if ref==markRef&&mode!="missing_mark_parent"{return markParent,nil}
    return uuid.Nil,ErrInvalid
   })
   result,err:=(DelegatedWatermarkSigningTaskResolver{Documents:docs,
    Producers:producer}).ResolveDelegatedTask(context.Background(),ready,task)
   if mode=="valid"{
    if err!=nil||len(result.Node.Parents)!=2||len(result.Node.Inputs)!=2{
     t.Fatal("delegated watermark ancestry lost",err)
    }
   }else if err==nil{t.Fatal("missing mark parent accepted")}
  })
 }
}

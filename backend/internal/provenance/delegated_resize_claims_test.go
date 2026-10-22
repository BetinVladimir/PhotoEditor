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
 workflow "photoeditor/backend/internal/encodecontract"
)

func TestDelegatedResizeClaimsBindGeometryAndReport(t *testing.T){
 fixture,err:=os.ReadFile("../review/testdata/working-images-python.json")
 if err!=nil{t.Fatal(err)}
 var images []json.RawMessage
 if json.Unmarshal(fixture,&images)!=nil||len(images)==0{t.Fatal("missing image")}
 for _,mode:=range []string{"valid","report"}{
  t.Run(mode,func(t *testing.T){
   source,err:=review.ParseWorkingImage(images[0])
   if err!=nil{t.Fatal(err)}
   sourceRaw,sourceRef:=contractBytes(t,source)
   edge:=int64(200)
   paramsRaw,paramsRef:=contractBytes(t,signingResizeParams{
    Image:sourceRef,ExportSize:workflow.ExportSize{LongEdge:&edge,Fit:"contain"}})
   pixel:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("b",64)}
   after:=source
   after.Pixels=review.ArtifactRef{ID:pixel.ArtifactID,SHA256:pixel.SHA256}
   space,err:=canonical.Hash(map[string]any{"image":sourceRef,
    "width":int64(200),"height":int64(150)})
   if err!=nil{t.Fatal(err)}
   after.Canvas=review.Canvas{Width:200,Height:150,Orientation:1,
    SpaceID:"resize:"+space[:32],SourceSHA256:source.Canvas.SourceSHA256}
   steps:=[]any{map[string]any{"kind":"resize","from_canvas":source.Canvas,
    "to_canvas":after.Canvas,"invertibility":"invertible",
    "valid_region":map[string]int64{"x":0,"y":0,"width":200,"height":150},
    "pixel_centers":"half_pixel"}}
   chainHash,err:=canonical.Hash(map[string]any{"steps":steps})
   if err!=nil{t.Fatal(err)}
   after.TransformChain,_=canonical.Marshal(map[string]any{"steps":steps,
    "hash":chainHash})
   resultRaw,resultRef:=contractBytes(t,after)
   report:=signingDerivativeReport{Image:sourceRef,Operation:"resize",
    Applied:true,Width:200,Height:150,Warnings:[]string{}}
   if mode=="report"{report.Width++}
   reportRaw,reportRef:=contractBytes(t,report)
   inputs,_:=canonical.Marshal([]v2input.Ref{sourceRef,
    {ArtifactID:source.Pixels.ID,SHA256:source.Pixels.SHA256}})
   outputs,_:=canonical.Marshal([]v2input.Ref{pixel,resultRef,reportRef})
   ready:=ReadySigningExport{Tenant:uuid.New(),Project:uuid.New(),
    ExportID:uuid.New(),Task:uuid.New(),Run:uuid.New()}
   task:=CanonicalSigningTask{Task:uuid.New(),Run:ready.Run,Result:uuid.New(),
    Attempt:1,Fence:1,Operation:"derivative.resize",Version:"1",
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
   parent:=uuid.New()
   producer:=delegatedImageProducerFunc(func(_ context.Context,
    ref v2input.Ref)(uuid.UUID,error){
    if ref!=sourceRef{return uuid.Nil,ErrInvalid}
    return parent,nil
   })
   result,err:=(DelegatedResizeSigningTaskResolver{Documents:docs,
    Producers:producer}).ResolveDelegatedTask(context.Background(),ready,task)
   if mode=="valid"{
    if err!=nil||len(result.Node.Parents)!=1||result.Node.Parents[0]!=parent{
     t.Fatal("delegated resize claim lost",err)
    }
   }else if err==nil{t.Fatal("invalid delegated report accepted")}
  })
 }
}

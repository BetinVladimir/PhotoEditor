package provenance

import (
 "bytes"
 "context"

 "github.com/google/uuid"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
 workflow "photoeditor/backend/internal/encodecontract"
)

type DelegatedTaskDocuments interface{
 ReadDelegatedTaskDocument(context.Context,CanonicalSigningTask,
  v2input.Ref,int)([]byte,error)
}
type DelegatedImageProducer interface{
 ResolveDelegatedImageProducer(context.Context,v2input.Ref)(uuid.UUID,error)
}

type DelegatedEncodeSigningTaskResolver struct{
 Documents DelegatedTaskDocuments
 Producers DelegatedImageProducer
}

func(s DelegatedEncodeSigningTaskResolver)ResolveDelegatedTask(ctx context.Context,
 ready ReadySigningExport,e CanonicalSigningTask)(ResolvedSigningTask,error){
 var zero ResolvedSigningTask
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Documents==nil||s.Producers==nil||ready.Tenant==uuid.Nil||
  ready.Project==uuid.Nil||ready.ExportID==uuid.Nil||
  e.Operation!="export.encode"||e.Version!="1"||e.ReviewRequired||
  normalizeCanonicalSigningTask(&e)!=nil{return zero,ErrInvalid}
 outputs,err:=canonicalTaskOutputRefs(e.Outputs)
 if err!=nil||len(outputs)!=2{return zero,ErrInvalid}
 if e.Task==ready.Task&&outputs[0]!=ready.Ref{return zero,ErrInvalid}
 paramsRaw,err:=s.Documents.ReadDelegatedTaskDocument(ctx,e,e.Params,1<<20)
 if err!=nil{return zero,err}
 paramsRaw=append([]byte(nil),paramsRaw...)
 var p signingEncodeParams
 if !pinned(paramsRaw,e.Params,1<<20)||
  decode(paramsRaw,&p,1<<20)!=nil||!ref(p.Image){return zero,ErrInvalid}
 mime:=map[string]string{"jpeg":"image/jpeg","png":"image/png"}[p.Format]
 if mime==""||e.Task==ready.Task&&mime!=ready.Object.ContentType{
  return zero,ErrInvalid
 }
 inputs,err:=canonicalTaskOutputRefs(e.Inputs)
 if err!=nil||len(inputs)<2||len(inputs)>3||inputs[0]!=p.Image{
  return zero,ErrInvalid
 }
 imageRaw,err:=s.Documents.ReadDelegatedTaskDocument(ctx,e,p.Image,1<<20)
 if err!=nil{return zero,err}
 imageRaw=append([]byte(nil),imageRaw...)
 if !pinned(imageRaw,p.Image,1<<20){return zero,ErrInvalid}
 image,err:=review.ParseWorkingImage(imageRaw)
 if err!=nil{return zero,ErrInvalid}
 refs:=make([]review.ArtifactRef,len(inputs))
 for i,r:=range inputs{refs[i]=review.ArtifactRef{ID:r.ArtifactID,SHA256:r.SHA256}}
 _,err=workflow.CompileDeliveryEncodeProfile(workflow.CompiledDownloadNode{
  Operation:e.Operation,Version:e.Version,ResourceClass:"cpu",
  Params:paramsRaw,Inputs:refs},imageRaw,workflow.DeliveryDerivativeLimits{
  RAMBytes:1<<53-1,ScratchBytes:1<<53-1,OutputBytes:1<<53-1})
 if err!=nil||image.Color.Encoding!="gamma24"{return zero,ErrInvalid}
 manifestRaw,err:=s.Documents.ReadDelegatedTaskDocument(ctx,e,outputs[1],1<<20)
 if err!=nil{return zero,err}
 manifestRaw=append([]byte(nil),manifestRaw...)
 var encoded signingEncodedImage
 if !pinned(manifestRaw,outputs[1],1<<20)||
  decode(manifestRaw,&encoded,1<<20)!=nil||
  !validSigningEncodedImage(encoded){return zero,ErrInvalid}
 width,height:=signingEncodeDimensions(image.Canvas.Width,image.Canvas.Height,p.Size)
 if encoded.Artifact!=outputs[0]||encoded.Format!=p.Format||
  encoded.ICCID!=p.ICCID||encoded.ICCSHA!=p.ICCSHA256||
  encoded.Policy!="strip"||encoded.Width!=width||encoded.Height!=height||
  encoded.Alpha!=(image.Alpha!=nil&&p.Alpha=="preserve"){
  return zero,ErrInvalid
 }
 producer,err:=s.Producers.ResolveDelegatedImageProducer(ctx,p.Image)
 if err!=nil{return zero,err}
 if producer==uuid.Nil||producer==e.Task{return zero,ErrInvalid}
 for _,document:=range []struct{ref v2input.Ref;raw []byte}{
  {e.Params,paramsRaw},{p.Image,imageRaw},{outputs[1],manifestRaw}}{
  current,err:=s.Documents.ReadDelegatedTaskDocument(ctx,e,document.ref,1<<20)
  if err!=nil{return zero,err}
  if !bytes.Equal(current,document.raw){return zero,ErrInvalid}
 }
 current,err:=s.Producers.ResolveDelegatedImageProducer(ctx,p.Image)
 if err!=nil{return zero,err}
 if current!=producer{return zero,ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err}
 return ResolvedSigningTask{Node:SigningExecutionNode{
  Task:e.Task,Run:e.Run,CanonicalResult:e.Result,Attempt:e.Attempt,
  Fence:e.Fence,Parents:[]uuid.UUID{producer},
  Inputs:[]v2input.Ref{p.Image},Outputs:outputs,
  Action:SigningAction{Operation:e.Operation,Version:e.Version,
   ParamsSHA:e.Params.SHA256}}},nil
}

var _ DelegatedTaskClaims = DelegatedEncodeSigningTaskResolver{}

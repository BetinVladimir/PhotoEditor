package provenance

import (
 "bytes"
 "context"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type DelegatedOriginalSource interface{
 QualifyDelegatedOriginal(context.Context,CanonicalSigningTask,
  v2input.Ref,v2input.Ref,int64)error
}

type DelegatedDecodeSigningTaskResolver struct{
 Documents DelegatedTaskDocuments
 Originals DelegatedOriginalSource
 WorkICCSHA string
}

func(s DelegatedDecodeSigningTaskResolver)ResolveDelegatedTask(ctx context.Context,
 ready ReadySigningExport,e CanonicalSigningTask)(ResolvedSigningTask,error){
 var zero ResolvedSigningTask
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Documents==nil||s.Originals==nil||!digest(s.WorkICCSHA)||
  ready.Tenant==uuid.Nil||ready.Project==uuid.Nil||
  ready.ExportID==uuid.Nil||e.Operation!="asset.decode"||
  e.Version!="1"||e.ReviewRequired||normalizeCanonicalSigningTask(&e)!=nil{
  return zero,ErrInvalid
 }
 paramsRaw,err:=s.Documents.ReadDelegatedTaskDocument(ctx,e,e.Params,1<<20)
 if err!=nil{return zero,err}
 paramsRaw=append([]byte(nil),paramsRaw...)
 if !pinned(paramsRaw,e.Params,1<<20){return zero,ErrInvalid}
 p,err:=decodeSigningDecodeParams(paramsRaw)
 if err!=nil{return zero,err}
 inputs,err:=canonicalTaskInputRefs(e.Inputs)
 if err!=nil||len(inputs)!=1||inputs[0]!=p.Source{return zero,ErrInvalid}
 outputs,err:=canonicalTaskOutputRefs(e.Outputs)
 if err!=nil||len(outputs)<2||len(outputs)>3{return zero,ErrInvalid}
 master:=outputs[len(outputs)-1]
 imageRaw,err:=s.Documents.ReadDelegatedTaskDocument(ctx,e,master,1<<20)
 if err!=nil{return zero,err}
 imageRaw=append([]byte(nil),imageRaw...)
 if !pinned(imageRaw,master,1<<20){return zero,ErrInvalid}
 image,err:=review.ParseWorkingImage(imageRaw)
 if err!=nil{return zero,ErrInvalid}
 if image.DType!="float32"||
  image.Color.ProfileID!="work_rec2020_gamma24_v1"||
  image.Color.Encoding!="gamma24"||
  image.Color.ICCSHA256!=s.WorkICCSHA||
  image.Canvas.SourceSHA256!=p.Source.SHA256||
  image.Canvas.Orientation!=1||
  image.Canvas.SpaceID!="oriented:"+p.Source.SHA256||
  image.Pixels.ID!=outputs[0].ArtifactID||
  image.Pixels.SHA256!=outputs[0].SHA256{return zero,ErrInvalid}
 if len(outputs)==2&&image.Alpha!=nil||
  len(outputs)==3&&(image.Alpha==nil||
   image.Alpha.ID!=outputs[1].ArtifactID||
   image.Alpha.SHA256!=outputs[1].SHA256)||
  p.Analyze&&image.Alpha!=nil{return zero,ErrInvalid}
 chainHash,err:=canonical.Hash(map[string]any{"steps":[]any{}})
 if err!=nil{return zero,err}
 chain,err:=canonical.Marshal(map[string]any{"steps":[]any{},
  "hash":chainHash})
 if err!=nil||!bytes.Equal(chain,image.TransformChain){return zero,ErrInvalid}
 if err:=s.Originals.QualifyDelegatedOriginal(ctx,e,p.Source,master,p.Bytes);
  err!=nil{return zero,err}
 for _,document:=range []struct{ref v2input.Ref;raw []byte}{
  {e.Params,paramsRaw},{master,imageRaw}}{
  current,err:=s.Documents.ReadDelegatedTaskDocument(ctx,e,document.ref,1<<20)
  if err!=nil{return zero,err}
  if !bytes.Equal(current,document.raw){return zero,ErrInvalid}
 }
 if err:=ctx.Err();err!=nil{return zero,err}
 return ResolvedSigningTask{Sources:[]v2input.Ref{p.Source},
  Node:SigningExecutionNode{Task:e.Task,Run:e.Run,CanonicalResult:e.Result,
   Attempt:e.Attempt,Fence:e.Fence,Parents:[]uuid.UUID{},
   Inputs:[]v2input.Ref{p.Source},Outputs:outputs,
   Action:SigningAction{Operation:e.Operation,Version:e.Version,
    ParamsSHA:e.Params.SHA256}}},nil
}

var _ DelegatedTaskClaims = DelegatedDecodeSigningTaskResolver{}

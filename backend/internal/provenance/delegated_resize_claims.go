package provenance

import (
 "bytes"
 "context"
 "slices"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type DelegatedResizeSigningTaskResolver struct{
 Documents DelegatedTaskDocuments
 Producers DelegatedImageProducer
}

func(s DelegatedResizeSigningTaskResolver)ResolveDelegatedTask(ctx context.Context,
 ready ReadySigningExport,e CanonicalSigningTask)(ResolvedSigningTask,error){
 var zero ResolvedSigningTask
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Documents==nil||s.Producers==nil||ready.Tenant==uuid.Nil||
  ready.Project==uuid.Nil||ready.ExportID==uuid.Nil||
  e.Operation!="derivative.resize"||e.Version!="1"||e.ReviewRequired||
  normalizeCanonicalSigningTask(&e)!=nil{return zero,ErrInvalid}
 documents:=map[v2input.Ref][]byte{}
 read:=func(ref v2input.Ref)([]byte,error){
  raw,err:=s.Documents.ReadDelegatedTaskDocument(ctx,e,ref,1<<20)
  if err!=nil{return nil,err}
  raw=append([]byte(nil),raw...)
  if !pinned(raw,ref,1<<20){return nil,ErrInvalid}
  documents[ref]=raw
  return raw,nil
 }
 paramsRaw,err:=read(e.Params)
 if err!=nil{return zero,err}
 p,err:=decodeSigningResizeParams(paramsRaw)
 if err!=nil{return zero,err}
 sourceRaw,err:=read(p.Image)
 if err!=nil{return zero,err}
 source,err:=review.ParseWorkingImage(sourceRaw)
 if err!=nil||source.DType!="float32"||
  source.Color.ProfileID!="work_rec2020_gamma24_v1"{
  return zero,ErrInvalid
 }
 inputs,err:=canonicalTaskOutputRefs(e.Inputs)
 if err!=nil{return zero,err}
 required:=[]v2input.Ref{p.Image,{ArtifactID:source.Pixels.ID,
  SHA256:source.Pixels.SHA256}}
 if source.Alpha!=nil{required=append(required,v2input.Ref{
  ArtifactID:source.Alpha.ID,SHA256:source.Alpha.SHA256})}
 if !slices.Equal(inputs,required){return zero,ErrInvalid}
 outputs,err:=canonicalTaskOutputRefs(e.Outputs)
 if err!=nil||len(outputs)!=len(required)+1{return zero,ErrInvalid}
 expected,err:=expectedSigningResizeImage(source,p,outputs)
 if err!=nil{return zero,err}
 resultRaw,err:=read(outputs[len(outputs)-2])
 if err!=nil{return zero,err}
 if _,err:=review.ParseWorkingImage(resultRaw);err!=nil{return zero,ErrInvalid}
 expectedRaw,err:=canonical.Marshal(expected)
 if err!=nil||!bytes.Equal(expectedRaw,resultRaw){return zero,ErrInvalid}
 reportRaw,err:=read(outputs[len(outputs)-1])
 if err!=nil{return zero,err}
 var report signingDerivativeReport
 if decode(reportRaw,&report,1<<20)!=nil{return zero,ErrInvalid}
 same:=expected.Canvas.Width==source.Canvas.Width&&
  expected.Canvas.Height==source.Canvas.Height
 warnings:=[]string{}
 if same{warnings=[]string{"no_resize_required"}}
 if report.Image!=p.Image||report.Operation!="resize"||
  report.Applied==same||report.Width!=expected.Canvas.Width||
  report.Height!=expected.Canvas.Height||report.MaxDelta!=0||
  report.Warnings==nil||!slices.Equal(report.Warnings,warnings){
  return zero,ErrInvalid
 }
 producer,err:=s.Producers.ResolveDelegatedImageProducer(ctx,p.Image)
 if err!=nil{return zero,err}
 if producer==uuid.Nil||producer==e.Task{return zero,ErrInvalid}
 for ref,raw:=range documents{
  current,err:=s.Documents.ReadDelegatedTaskDocument(ctx,e,ref,1<<20)
  if err!=nil{return zero,err}
  if !bytes.Equal(current,raw){return zero,ErrInvalid}
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

var _ DelegatedTaskClaims = DelegatedResizeSigningTaskResolver{}

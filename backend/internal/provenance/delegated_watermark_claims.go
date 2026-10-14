package provenance

import (
 "bytes"
 "context"
 "slices"
 "sort"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type DelegatedWatermarkSigningTaskResolver struct{
 Documents DelegatedTaskDocuments
 Producers DelegatedImageProducer
}

func(s DelegatedWatermarkSigningTaskResolver)ResolveDelegatedTask(ctx context.Context,
 ready ReadySigningExport,e CanonicalSigningTask)(ResolvedSigningTask,error){
 var zero ResolvedSigningTask
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Documents==nil||s.Producers==nil||ready.Tenant==uuid.Nil||
  ready.Project==uuid.Nil||ready.ExportID==uuid.Nil||
  e.Operation!="derivative.watermark"||e.Version!="1"||e.ReviewRequired||
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
 p,err:=decodeSigningWatermarkParams(paramsRaw)
 if err!=nil{return zero,err}
 sourceRaw,err:=read(p.Image)
 if err!=nil{return zero,err}
 source,err:=review.ParseWorkingImage(sourceRaw)
 if err!=nil{return zero,ErrInvalid}
 markRaw,err:=read(p.Watermark)
 if err!=nil{return zero,err}
 mark,err:=review.ParseWorkingImage(markRaw)
 if err!=nil{return zero,ErrInvalid}
 if source.DType!="float32"||mark.DType!="float32"||
  source.Color.ProfileID!="work_rec2020_gamma24_v1"||
  mark.Color!=source.Color||mark.Alpha==nil||
  !signingWatermarkFits(source.Canvas,mark.Canvas,p){return zero,ErrInvalid}
 required:=func(ref v2input.Ref,image review.WorkingImage)[]v2input.Ref{
  refs:=[]v2input.Ref{ref,{ArtifactID:image.Pixels.ID,
   SHA256:image.Pixels.SHA256}}
  if image.Alpha!=nil{refs=append(refs,v2input.Ref{
   ArtifactID:image.Alpha.ID,SHA256:image.Alpha.SHA256})}
  return refs
 }
 inputs,err:=canonicalTaskInputRefs(e.Inputs)
 if err!=nil||!slices.Equal(inputs,
  append(required(p.Image,source),required(p.Watermark,mark)...)){
  return zero,ErrInvalid
 }
 outputs,err:=canonicalTaskOutputRefs(e.Outputs)
 count:=3
 if source.Alpha!=nil{count=4}
 if err!=nil||len(outputs)!=count{return zero,ErrInvalid}
 resultRaw,err:=read(outputs[count-2])
 if err!=nil{return zero,err}
 if _,err:=review.ParseWorkingImage(resultRaw);err!=nil{return zero,ErrInvalid}
 expected:=source
 expected.Pixels=review.ArtifactRef{ID:outputs[0].ArtifactID,
  SHA256:outputs[0].SHA256}
 if source.Alpha!=nil{expected.Alpha=&review.ArtifactRef{
  ID:outputs[1].ArtifactID,SHA256:outputs[1].SHA256}}
 expectedRaw,err:=canonical.Marshal(expected)
 if err!=nil||!bytes.Equal(expectedRaw,resultRaw){return zero,ErrInvalid}
 reportRaw,err:=read(outputs[count-1])
 if err!=nil{return zero,err}
 var report signingDerivativeReport
 if decode(reportRaw,&report,1<<20)!=nil{return zero,ErrInvalid}
 warnings:=[]string{}
 if p.Opacity==0{warnings=[]string{"zero_opacity"}}
 if report.Image!=p.Image||report.Operation!="watermark"||
  report.Applied!=(p.Opacity>0)||report.Width!=source.Canvas.Width||
  report.Height!=source.Canvas.Height||
  !finiteSigningNumber(report.MaxDelta,0,1)||report.Warnings==nil||
  !slices.Equal(report.Warnings,warnings)||
  p.Opacity==0&&report.MaxDelta!=0{return zero,ErrInvalid}
 content:=[]v2input.Ref{p.Image}
 if p.Watermark!=p.Image{content=append(content,p.Watermark)}
 origins:=map[v2input.Ref]uuid.UUID{}
 parents:=[]uuid.UUID{}
 for _,ref:=range content{
  producer,err:=s.Producers.ResolveDelegatedImageProducer(ctx,ref)
  if err!=nil{return zero,err}
  if producer==uuid.Nil||producer==e.Task{return zero,ErrInvalid}
  origins[ref]=producer
  if !slices.Contains(parents,producer){parents=append(parents,producer)}
 }
 for ref,raw:=range documents{
  current,err:=s.Documents.ReadDelegatedTaskDocument(ctx,e,ref,1<<20)
  if err!=nil{return zero,err}
  if !bytes.Equal(current,raw){return zero,ErrInvalid}
 }
 for ref,producer:=range origins{
  current,err:=s.Producers.ResolveDelegatedImageProducer(ctx,ref)
  if err!=nil{return zero,err}
  if current!=producer{return zero,ErrInvalid}
 }
 if err:=ctx.Err();err!=nil{return zero,err}
 sort.Slice(parents,func(i,j int)bool{return parents[i].String()<parents[j].String()})
 return ResolvedSigningTask{Node:SigningExecutionNode{
  Task:e.Task,Run:e.Run,CanonicalResult:e.Result,Attempt:e.Attempt,
  Fence:e.Fence,Parents:parents,Inputs:content,Outputs:outputs,
  Action:SigningAction{Operation:e.Operation,Version:e.Version,
   ParamsSHA:e.Params.SHA256}}},nil
}

var _ DelegatedTaskClaims = DelegatedWatermarkSigningTaskResolver{}

package provenance

import (
 "bytes"
 "context"
 "slices"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

type DelegatedMetadataSigningTaskResolver struct{
 Documents DelegatedTaskDocuments
 Producers DelegatedImageProducer
}

func(s DelegatedMetadataSigningTaskResolver)ResolveDelegatedTask(ctx context.Context,
 ready ReadySigningExport,e CanonicalSigningTask)(ResolvedSigningTask,error){
 var zero ResolvedSigningTask
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Documents==nil||s.Producers==nil||ready.Tenant==uuid.Nil||
  ready.Project==uuid.Nil||ready.ExportID==uuid.Nil||
  e.Task!=ready.Task||e.Run!=ready.Run||e.Operation!="metadata.write"||
  e.Version!="1"||e.ReviewRequired||normalizeCanonicalSigningTask(&e)!=nil{
  return zero,ErrInvalid
 }
 outputs,err:=canonicalTaskOutputRefs(e.Outputs)
 if err!=nil||len(outputs)!=3||outputs[0]!=ready.Ref{return zero,ErrInvalid}
 documents:=map[v2input.Ref][]byte{}
 read:=func(ref v2input.Ref,out any)error{
  raw,err:=s.Documents.ReadDelegatedTaskDocument(ctx,e,ref,1<<20)
  if err!=nil{return err}
  raw=append([]byte(nil),raw...)
  if !pinned(raw,ref,1<<20)||decode(raw,out,1<<20)!=nil{return ErrInvalid}
  documents[ref]=raw
  return nil
 }
 var p signingMetadataParams
 if err:=read(e.Params,&p);err!=nil{return zero,err}
 if !ref(p.Encoded)||!ref(p.Document)||p.Encoded==p.Document||
  p.ToolID!="exiftool_v1"||
  p.Policy!="copyright_only"&&p.Policy!="safe_camera"||
  p.CopyGPS&&p.Policy!="safe_camera"{return zero,ErrInvalid}
 var before,after signingEncodedImage
 var document signingMetadataDocument
 var report signingMetadataReport
 if err:=read(p.Encoded,&before);err!=nil{return zero,err}
 if !validSigningEncodedImage(before)||before.Policy!="strip"{
  return zero,ErrInvalid
 }
 mime:=map[string]string{"jpeg":"image/jpeg","png":"image/png"}[before.Format]
 if mime==""||mime!=ready.Object.ContentType{return zero,ErrInvalid}
 inputs,err:=canonicalTaskOutputRefs(e.Inputs)
 if err!=nil||len(inputs)!=3||inputs[0]!=p.Encoded||
  inputs[1]!=before.Artifact||inputs[2]!=p.Document{return zero,ErrInvalid}
 if err:=read(p.Document,&document);err!=nil{return zero,err}
 copied,err:=expectedSigningMetadataFields(document,p)
 if err!=nil{return zero,err}
 if err:=read(outputs[1],&after);err!=nil{return zero,err}
 expected:=before
 expected.Artifact=ready.Ref
 expected.Policy=p.Policy
 if after!=expected||!validSigningEncodedImage(after)||
  after.Artifact==before.Artifact{return zero,ErrInvalid}
 if err:=read(outputs[2],&report);err!=nil{return zero,err}
 if report.Encoded!=p.Encoded||report.Document!=p.Document||
  report.Policy!=p.Policy||report.Copied==nil||
  !slices.Equal(report.Copied,copied)||report.Orientation!=1||
  !report.PreviewsRemoved||!report.ICCPreserved{return zero,ErrInvalid}
 producer,err:=s.Producers.ResolveDelegatedImageProducer(ctx,p.Encoded)
 if err!=nil{return zero,err}
 if producer==uuid.Nil||producer==e.Task{return zero,ErrInvalid}
 for ref,raw:=range documents{
  current,err:=s.Documents.ReadDelegatedTaskDocument(ctx,e,ref,1<<20)
  if err!=nil{return zero,err}
  if !bytes.Equal(current,raw){return zero,ErrInvalid}
 }
 current,err:=s.Producers.ResolveDelegatedImageProducer(ctx,p.Encoded)
 if err!=nil{return zero,err}
 if current!=producer{return zero,ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err}
 return ResolvedSigningTask{Node:SigningExecutionNode{
  Task:e.Task,Run:e.Run,CanonicalResult:e.Result,Attempt:e.Attempt,
  Fence:e.Fence,Parents:[]uuid.UUID{producer},
  Inputs:[]v2input.Ref{p.Encoded},Outputs:outputs,
  Action:SigningAction{Operation:e.Operation,Version:e.Version,
   ParamsSHA:e.Params.SHA256}}},nil
}

var _ DelegatedTaskClaims = DelegatedMetadataSigningTaskResolver{}

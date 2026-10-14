package delivery

import (
 "bytes"
 "context"
 "encoding/json"
 "io"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type DelegatedParentReportReader struct {
 Tasks SQLDelegatedTaskCatalog
 Documents DelegatedTaskDocumentReader
}

type delegatedParentReportRow struct{
 Task uuid.UUID `json:"task"`
 Root uuid.UUID `json:"root"`
 RootSHA string `json:"root_sha"`
 Bucket string `json:"bucket"`
 Key string `json:"key"`
 Version string `json:"version"`
 SHA256 string `json:"sha256"`
 Bytes int64 `json:"bytes"`
 MediaType string `json:"media_type"`
}

func(s DelegatedParentReportReader)lookup(ctx context.Context,
 ref v2input.Ref)(delegatedParentReportRow,error){
 var row delegatedParentReportRow
 if err:=ctx.Err();err!=nil{return row,err}
 if s.Tasks.DB==nil||ref.ArtifactID==uuid.Nil||
  !shaPattern.MatchString(ref.SHA256){return row,v2input.ErrInvalid}
 var raw []byte
 err:=s.Tasks.DB.QueryRow(ctx,`SELECT photo.v31_delivery_parent_report(
  $1,$2,$3,$4,$5,$6,$7)`,s.Tasks.Lease.Expansion,s.Tasks.Lease.Token,
  s.Tasks.Slot.Ordinal,s.Tasks.Slot.PresetID,ref.ArtifactID,ref.SHA256,
  s.Tasks.Bucket).Scan(&raw)
 if err!=nil{return row,err}
 if len(raw)<2||len(raw)>4096||json.Unmarshal(raw,&row)!=nil||
  row.Task==uuid.Nil||row.Root==uuid.Nil||
  !shaPattern.MatchString(row.RootSHA)||row.Bucket!=s.Tasks.Bucket||
  row.Key==""||len(row.Key)>2048||row.Version==""||
  row.Version=="null"||len(row.Version)>1024||
  row.SHA256!=ref.SHA256||row.Bytes<2||row.Bytes>1<<20||
  row.MediaType!="application/json"{
  return delegatedParentReportRow{},v2input.ErrInvalid
 }
 return row,nil
}

func(s DelegatedParentReportReader)ReadSigningImageReport(ctx context.Context,
 ref v2input.Ref)([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s.Tasks.DB==nil||s.Documents.Objects==nil||
  s.Documents.Tasks.DB!=s.Tasks.DB||
  s.Documents.Tasks.Lease!=s.Tasks.Lease||
  s.Documents.Tasks.Scope!=s.Tasks.Scope||
  s.Documents.Tasks.Ready!=s.Tasks.Ready||
  s.Documents.Tasks.Slot.Ordinal!=s.Tasks.Slot.Ordinal||
  s.Documents.Tasks.Slot.PresetID!=s.Tasks.Slot.PresetID||
  s.Documents.Tasks.Bucket!=s.Tasks.Bucket{return nil,v2input.ErrInvalid}
 first,err:=s.lookup(ctx,ref)
 if err!=nil{return nil,err}
 task,err:=s.Tasks.ReadDelegatedTask(ctx,first.Task)
 if err!=nil{return nil,err}
 root:=v2input.Ref{ArtifactID:first.Root,SHA256:first.RootSHA}
 if !delegatedTaskHasOnlyOutput(task,root){return nil,v2input.ErrInvalid}
 rootRaw,err:=s.Documents.ReadDelegatedTaskDocument(ctx,task,root,1<<20)
 if err!=nil{return nil,err}
 result,err:=v2draft.ParseImageResult(rootRaw,root)
 if err!=nil||result.Report!=ref{return nil,v2input.ErrInvalid}
 raw,err:=s.readReport(ctx,first,ref)
 if err!=nil{return nil,err}
 body:=append([]byte(nil),raw...)
 if _,err:=v2draft.ParseImageReport(body,ref);err!=nil{
  return nil,v2input.ErrInvalid
 }
 currentRoot,err:=s.Documents.ReadDelegatedTaskDocument(ctx,task,root,1<<20)
 if err!=nil||!bytes.Equal(rootRaw,currentRoot){return nil,v2input.ErrInvalid}
 currentReport,err:=s.readReport(ctx,first,ref)
 if err!=nil||!bytes.Equal(body,currentReport){return nil,v2input.ErrInvalid}
 second,err:=s.lookup(ctx,ref)
 if err!=nil{return nil,err}
 firstRaw,err:=canonical.Marshal(first)
 if err!=nil{return nil,err}
 secondRaw,err:=canonical.Marshal(second)
 if err!=nil||!bytes.Equal(firstRaw,secondRaw){return nil,v2input.ErrInvalid}
 currentTask,err:=s.Tasks.ReadDelegatedTask(ctx,first.Task)
 if err!=nil{return nil,err}
 taskRaw,err:=canonical.Marshal(task)
 if err!=nil{return nil,err}
 currentRaw,err:=canonical.Marshal(currentTask)
 if err!=nil||!bytes.Equal(taskRaw,currentRaw){return nil,v2input.ErrInvalid}
 return body,ctx.Err()
}

func(s DelegatedParentReportReader)readReport(ctx context.Context,
 row delegatedParentReportRow,ref v2input.Ref)([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 object:=objectstore.Object{Key:row.Key,VersionID:row.Version,
  SHA256:row.SHA256,Bytes:row.Bytes,ContentType:row.MediaType}
 stream,err:=s.Documents.Objects.Open(ctx,object)
 if err!=nil{
  if stream!=nil{_ = stream.Close()}
  return nil,err
 }
 if stream==nil{return nil,v2input.ErrInvalid}
 raw,readErr:=io.ReadAll(io.LimitReader(delegatedContextReader{ctx,stream},row.Bytes+1))
 closeErr:=stream.Close()
 if err:=ctx.Err();err!=nil{return nil,err}
 if readErr!=nil{return nil,readErr}
 if closeErr!=nil{return nil,closeErr}
 if int64(len(raw))!=row.Bytes||digest(raw)!=ref.SHA256{
  return nil,v2input.ErrInvalid
 }
 normalized,err:=canonical.Bytes(raw)
 if err!=nil||!bytes.Equal(normalized,raw){return nil,v2input.ErrInvalid}
 current,err:=s.lookup(ctx,ref)
 if err!=nil{return nil,err}
 original,err:=canonical.Marshal(row)
 if err!=nil{return nil,err}
 repeated,err:=canonical.Marshal(current)
 if err!=nil||!bytes.Equal(original,repeated){return nil,v2input.ErrInvalid}
 return raw,ctx.Err()
}

var _ provenance.SigningImageReportReader=DelegatedParentReportReader{}

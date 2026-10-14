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
 "photoeditor/backend/internal/v2input"
)

type DelegatedTaskDocumentReader struct {
 Tasks SQLDelegatedTaskCatalog
 Objects provenance.SigningObjects
}

func(s DelegatedTaskDocumentReader)lookup(ctx context.Context,
 task provenance.CanonicalSigningTask,ref v2input.Ref,max int)(objectstore.Object,error){
 var zero objectstore.Object
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Tasks.DB==nil||task.Task==uuid.Nil||ref.ArtifactID==uuid.Nil||
  !shaPattern.MatchString(ref.SHA256)||max<2||max>8<<20||
  len(s.Tasks.Bucket)<3||len(s.Tasks.Bucket)>63{return zero,v2input.ErrInvalid}
 expected,err:=canonical.Marshal(task)
 if err!=nil{return zero,v2input.ErrInvalid}
 current,err:=s.Tasks.ReadDelegatedTask(ctx,task.Task)
 if err!=nil{return zero,err}
 currentRaw,err:=canonical.Marshal(current)
 if err!=nil||!bytes.Equal(expected,currentRaw){return zero,v2input.ErrInvalid}
 var raw []byte
 err=s.Tasks.DB.QueryRow(ctx,`SELECT photo.v31_delivery_task_document(
  $1,$2,$3,$4,$5,$6,$7,$8,$9)`,s.Tasks.Lease.Expansion,
  s.Tasks.Lease.Token,s.Tasks.Slot.Ordinal,s.Tasks.Slot.PresetID,
  task.Task,ref.ArtifactID,ref.SHA256,max,s.Tasks.Bucket).Scan(&raw)
 if err!=nil{return zero,err}
 var wire struct{
  Bucket string `json:"bucket"`
  Key string `json:"key"`
  Version string `json:"version"`
  SHA256 string `json:"sha256"`
  Bytes int64 `json:"bytes"`
  MediaType string `json:"media_type"`
 }
 if len(raw)<2||len(raw)>4096||json.Unmarshal(raw,&wire)!=nil||
  wire.Bucket!=s.Tasks.Bucket||wire.Key==""||wire.Version==""||
  wire.Version=="null"||wire.SHA256!=ref.SHA256||
  wire.Bytes<2||wire.Bytes>int64(max)||wire.MediaType!="application/json"{
  return zero,v2input.ErrInvalid
 }
 current,err=s.Tasks.ReadDelegatedTask(ctx,task.Task)
 if err!=nil{return zero,err}
 currentRaw,err=canonical.Marshal(current)
 if err!=nil||!bytes.Equal(expected,currentRaw){return zero,v2input.ErrInvalid}
 return objectstore.Object{Key:wire.Key,VersionID:wire.Version,
  SHA256:wire.SHA256,Bytes:wire.Bytes,ContentType:wire.MediaType},nil
}

// ReadDelegatedTaskDocument checks the SQL role, exact versioned object bytes
// and unchanged role/evidence after object IO.
func(s DelegatedTaskDocumentReader)ReadDelegatedTaskDocument(ctx context.Context,
 task provenance.CanonicalSigningTask,ref v2input.Ref,max int)([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s.Objects==nil{return nil,v2input.ErrInvalid}
 object,err:=s.lookup(ctx,task,ref,max)
 if err!=nil{return nil,err}
 stream,err:=s.Objects.Open(ctx,object)
 if err!=nil{
  if stream!=nil{_ = stream.Close()}
  return nil,err
 }
 if stream==nil{return nil,v2input.ErrInvalid}
 raw,readErr:=io.ReadAll(io.LimitReader(delegatedContextReader{ctx,stream},object.Bytes+1))
 closeErr:=stream.Close()
 if err:=ctx.Err();err!=nil{return nil,err}
 if readErr!=nil{return nil,readErr}
 if closeErr!=nil{return nil,closeErr}
 if int64(len(raw))!=object.Bytes||digest(raw)!=ref.SHA256{
  return nil,v2input.ErrInvalid
 }
 normalized,err:=canonical.Bytes(raw)
 if err!=nil||!bytes.Equal(normalized,raw){return nil,v2input.ErrInvalid}
 current,err:=s.lookup(ctx,task,ref,max)
 if err!=nil{return nil,err}
 if current!=object{return nil,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return nil,err}
 return raw,nil
}

type delegatedContextReader struct{ctx context.Context;reader io.Reader}
func(r delegatedContextReader)Read(p []byte)(int,error){
 if err:=r.ctx.Err();err!=nil{return 0,err}
 n,err:=r.reader.Read(p)
 if cancelled:=r.ctx.Err();cancelled!=nil{return n,cancelled}
 return n,err
}

package delivery

import (
 "bytes"
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "io"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2input"
)

type DelegatedOriginalQualifier struct{
 Tasks SQLDelegatedTaskCatalog
 Objects provenance.SigningObjects
}

func(s DelegatedOriginalQualifier)lookup(ctx context.Context,
 task provenance.CanonicalSigningTask,original,master v2input.Ref,
 expectedBytes int64)(objectstore.Object,error){
 var zero objectstore.Object
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Tasks.DB==nil||task.Task==uuid.Nil||task.Operation!="asset.decode"||
  task.Version!="1"||original.ArtifactID==uuid.Nil||
  master.ArtifactID==uuid.Nil||original==master||
  !shaPattern.MatchString(original.SHA256)||
  !shaPattern.MatchString(master.SHA256)||
  expectedBytes<1||expectedBytes>512<<20||
  len(s.Tasks.Bucket)<3||len(s.Tasks.Bucket)>63{
  return zero,v2input.ErrInvalid
 }
 expectedTask,err:=canonical.Marshal(task)
 if err!=nil{return zero,v2input.ErrInvalid}
 currentTask,err:=s.Tasks.ReadDelegatedTask(ctx,task.Task)
 if err!=nil{return zero,err}
 currentRaw,err:=canonical.Marshal(currentTask)
 if err!=nil||!bytes.Equal(expectedTask,currentRaw){return zero,v2input.ErrInvalid}
 var raw []byte
 err=s.Tasks.DB.QueryRow(ctx,`SELECT photo.v31_delivery_original_source(
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,s.Tasks.Lease.Expansion,
  s.Tasks.Lease.Token,s.Tasks.Slot.Ordinal,s.Tasks.Slot.PresetID,
  task.Task,original.ArtifactID,original.SHA256,master.ArtifactID,
  master.SHA256,s.Tasks.Bucket).Scan(&raw)
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
  wire.Version=="null"||wire.SHA256!=original.SHA256||
  wire.Bytes!=expectedBytes||wire.MediaType==""||
  wire.MediaType=="application/json"{
  return zero,v2input.ErrInvalid
 }
 currentTask,err=s.Tasks.ReadDelegatedTask(ctx,task.Task)
 if err!=nil{return zero,err}
 currentRaw,err=canonical.Marshal(currentTask)
 if err!=nil||!bytes.Equal(expectedTask,currentRaw){return zero,v2input.ErrInvalid}
 return objectstore.Object{Key:wire.Key,VersionID:wire.Version,
  SHA256:wire.SHA256,Bytes:wire.Bytes,ContentType:wire.MediaType},nil
}

func(s DelegatedOriginalQualifier)QualifyDelegatedOriginal(ctx context.Context,
 task provenance.CanonicalSigningTask,original,master v2input.Ref,
 expectedBytes int64)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.Objects==nil{return v2input.ErrInvalid}
 object,err:=s.lookup(ctx,task,original,master,expectedBytes)
 if err!=nil{return err}
 stream,err:=s.Objects.Open(ctx,object)
 if err!=nil{
  if stream!=nil{_ = stream.Close()}
  return err
 }
 if stream==nil{return v2input.ErrInvalid}
 hash:=sha256.New()
 size,readErr:=io.Copy(hash,io.LimitReader(
  delegatedContextReader{ctx,stream},expectedBytes+1))
 closeErr:=stream.Close()
 if err:=ctx.Err();err!=nil{return err}
 if readErr!=nil{return readErr}
 if closeErr!=nil{return closeErr}
 if size!=expectedBytes||hex.EncodeToString(hash.Sum(nil))!=original.SHA256{
  return v2input.ErrInvalid
 }
 current,err:=s.lookup(ctx,task,original,master,expectedBytes)
 if err!=nil{return err}
 if current!=object{return v2input.ErrInvalid}
 return ctx.Err()
}

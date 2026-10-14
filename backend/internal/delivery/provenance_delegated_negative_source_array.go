package delivery

import (
 "bytes"
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

type DelegatedNegativeSourceArrayCatalog struct {
 Tasks SQLDelegatedTaskCatalog
 Documents DelegatedTaskDocumentReader
 Adopted DelegatedAdoptedResultReader
}

func (s DelegatedNegativeSourceArrayCatalog) LookupDelegatedNegativeSourceArray(
 ctx context.Context, task provenance.CanonicalSigningTask,
 evidence provenance.SigningNegativePixelEvidence, role string) (objectstore.Object,error) {
 var zero objectstore.Object
 if err := ctx.Err(); err != nil { return zero,err }
 if s.Tasks.DB==nil || s.Documents.Objects==nil ||
  s.Documents.Tasks.DB!=s.Tasks.DB || s.Adopted.Tasks.DB!=s.Tasks.DB ||
  s.Documents.Tasks.Lease!=s.Tasks.Lease || s.Adopted.Tasks.Lease!=s.Tasks.Lease ||
  s.Documents.Tasks.Scope!=s.Tasks.Scope || s.Adopted.Tasks.Scope!=s.Tasks.Scope ||
  s.Documents.Tasks.Ready!=s.Tasks.Ready || s.Adopted.Tasks.Ready!=s.Tasks.Ready ||
  s.Documents.Tasks.Slot.Ordinal!=s.Tasks.Slot.Ordinal ||
  s.Adopted.Tasks.Slot.Ordinal!=s.Tasks.Slot.Ordinal ||
  s.Documents.Tasks.Slot.PresetID!=s.Tasks.Slot.PresetID ||
  s.Adopted.Tasks.Slot.PresetID!=s.Tasks.Slot.PresetID ||
  s.Documents.Tasks.Bucket!=s.Tasks.Bucket || s.Adopted.Tasks.Bucket!=s.Tasks.Bucket ||
  task.Task==uuid.Nil || task.Operation!="optical.negative" || task.Version!="1" {
  return zero,v2input.ErrInvalid
 }
 spec,err:=provenance.ResolveDelegatedNegativeSourceArraySpec(task,evidence,role)
 if err!=nil{return zero,err}
 // The historical array pin is accepted only through current physical Params
 // and source WorkingImage bytes, never by a caller-supplied array ref alone.
 root,_,err:=s.Adopted.ReadDelegatedAdoptedResult(ctx,task)
 if err!=nil{return zero,err}
 paramsRaw,err:=s.Documents.ReadDelegatedTaskDocument(ctx,task,task.Params,65536)
 if err!=nil{return zero,err}
 params,err:=v2optical.ParseNegativeParams(paramsRaw)
 if err!=nil{return zero,err}
 sourceRaw,err:=s.Documents.ReadDelegatedTaskDocument(ctx,task,params.Base.Source,65536)
 if err!=nil{return zero,err}
 source,err:=v2optical.VerifyNegativeWorkingImage(params,sourceRaw)
 if err!=nil{return zero,err}
 expectedRaw,err:=canonical.Marshal(source)
 if err!=nil{return zero,err}
 evidenceSource,err:=provenance.DelegatedNegativeSourceDocument(evidence)
 if err!=nil||!bytes.Equal(expectedRaw,evidenceSource){return zero,v2input.ErrInvalid}
 size:=4*spec.Width*spec.Height*int64(spec.Channels)
 if size<4||size>2400000000{return zero,v2input.ErrInvalid}
 var raw []byte
 err=s.Tasks.DB.QueryRow(ctx,`SELECT photo.v31_delivery_negative_source_array(
  $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
  s.Tasks.Lease.Expansion,s.Tasks.Lease.Token,s.Tasks.Slot.Ordinal,
  s.Tasks.Slot.PresetID,task.Task,params.Base.Source.ArtifactID,
  params.Base.Source.SHA256,spec.Ref.ArtifactID,spec.Ref.SHA256,
  s.Tasks.Bucket,size).Scan(&raw)
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
  wire.Version=="null"||wire.SHA256!=spec.Ref.SHA256||
  wire.Bytes<size||wire.Bytes>size+65536||
  wire.MediaType!="application/x-npy"{return zero,v2input.ErrInvalid}
 repeatedRoot,_,err:=s.Adopted.ReadDelegatedAdoptedResult(ctx,task)
 if err!=nil||repeatedRoot!=root{return zero,v2input.ErrInvalid}
 currentParams,err:=s.Documents.ReadDelegatedTaskDocument(ctx,task,task.Params,65536)
 if err!=nil||!bytes.Equal(currentParams,paramsRaw){return zero,v2input.ErrInvalid}
 currentSource,err:=s.Documents.ReadDelegatedTaskDocument(ctx,task,params.Base.Source,65536)
 if err!=nil||!bytes.Equal(currentSource,sourceRaw){return zero,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err}
 return objectstore.Object{Key:wire.Key,VersionID:wire.Version,
  SHA256:wire.SHA256,Bytes:wire.Bytes,ContentType:wire.MediaType},nil
}

var _ provenance.DelegatedNegativeSourceArrayCatalog=DelegatedNegativeSourceArrayCatalog{}

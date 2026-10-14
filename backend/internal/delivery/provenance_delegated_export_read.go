package delivery

import (
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2input"
)

type delegatedExportEvidence struct {
 TenantID uuid.UUID `json:"tenant_id"`
 ProjectID uuid.UUID `json:"project_id"`
 ExportID uuid.UUID `json:"export_id"`
 VersionID uuid.UUID `json:"version_id"`
 TaskID uuid.UUID `json:"task_id"`
 RunID uuid.UUID `json:"run_id"`
 ArtifactID uuid.UUID `json:"artifact_id"`
 SHA256 string `json:"sha256"`
 Bucket string `json:"bucket"`
 ObjectKey string `json:"object_key"`
 ObjectVersion string `json:"object_version"`
 Bytes int64 `json:"bytes"`
 MediaType string `json:"media_type"`
}

func readyFromDelegatedEvidence(scope SignerDelegationScope,raw []byte,
 bucket string)(provenance.ReadySigningExport,error){
 var value delegatedExportEvidence
 if len(raw)<2||len(raw)>4096||json.Unmarshal(raw,&value)!=nil||
  value.TenantID!=scope.TenantID||value.ProjectID!=scope.ProjectID||
  value.ExportID!=scope.ExportID||value.ArtifactID!=scope.ExportArtifactID||
  value.SHA256!=scope.ExportSHA||value.VersionID==uuid.Nil||
  value.TaskID==uuid.Nil||value.RunID==uuid.Nil||
  value.Bucket!=bucket||value.ObjectKey==""||
  value.ObjectVersion==""||value.ObjectVersion=="null"||
  value.Bytes<1||value.Bytes>512<<20||
  value.MediaType!="image/jpeg"&&value.MediaType!="image/png"{
  return provenance.ReadySigningExport{},v2input.ErrInvalid
 }
 return provenance.ReadySigningExport{Tenant:value.TenantID,Project:value.ProjectID,
  ExportID:value.ExportID,Version:value.VersionID,Task:value.TaskID,Run:value.RunID,
  Ref:v2input.Ref{ArtifactID:value.ArtifactID,SHA256:value.SHA256},
  Object:objectstore.Object{Key:value.ObjectKey,VersionID:value.ObjectVersion,
   SHA256:value.SHA256,ContentType:value.MediaType,Bytes:value.Bytes}},nil
}

// ReadDelegatedReadyExport returns the current V31 source object and repeats
// its parent delegation before exposing evidence to object IO.
func ReadDelegatedReadyExport(ctx context.Context,db *pgxpool.Pool,
 lease ExpansionLease,slot Slot,bucket string)(SignerDelegationScope,
 provenance.ReadySigningExport,error){
 var zero provenance.ReadySigningExport
 scope,err:=ReadSignerDelegationScope(ctx,db,lease,slot)
 if err!=nil{return SignerDelegationScope{},zero,err}
 var raw []byte
 err=db.QueryRow(ctx,`SELECT photo.v31_delivery_export_evidence($1,$2,$3,$4)`,
  lease.Expansion,lease.Token,slot.Ordinal,slot.PresetID).Scan(&raw)
 if err!=nil{return SignerDelegationScope{},zero,err}
 ready,err:=readyFromDelegatedEvidence(scope,raw,bucket)
 if err!=nil{return SignerDelegationScope{},zero,err}
 current,err:=ReadSignerDelegationScope(ctx,db,lease,slot)
 if err!=nil{return SignerDelegationScope{},zero,err}
 if current!=scope{return SignerDelegationScope{},zero,v2input.ErrInvalid}
 return scope,ready,nil
}

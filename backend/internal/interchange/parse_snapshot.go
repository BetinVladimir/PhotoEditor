package interchange

import (
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

type ArtifactItem struct {
 Ordinal int `json:"ordinal"`
 Artifact v2input.Ref `json:"artifact"`
 Contract string `json:"contract"`
 OwnerID *uuid.UUID `json:"owner_id"`
}
type ArtifactSnapshot struct {
 SchemaVersion int `json:"schema_version"`
 SnapshotID uuid.UUID `json:"snapshot_id"`
 SnapshotDigest string `json:"snapshot_digest"`
 ProjectID uuid.UUID `json:"project_id"`
 Items []ArtifactItem `json:"items"`
}
type SnapshotDocument struct {Snapshot ArtifactSnapshot;Ref v2input.Ref;Body []byte}

func(p ParsePlan)CompileSnapshot()(SnapshotDocument,error){
 if p.identity.Snapshot==uuid.Nil||p.identity.Workflow==uuid.Nil||p.params.Request.ProjectID==uuid.Nil||p.params.Request.Bundle.ArtifactID==uuid.Nil||p.archiveBytes<1{return SnapshotDocument{},domain.NewError("VALIDATION_FAILED",422)}
 items:=[]ArtifactItem{{Ordinal:0,Artifact:p.params.Request.Bundle,Contract:"InterchangeArchive",OwnerID:nil}}
 unsigned:=struct{
  SchemaVersion int `json:"schema_version"`;SnapshotID uuid.UUID `json:"snapshot_id"`;ProjectID uuid.UUID `json:"project_id"`;Items []ArtifactItem `json:"items"`
 }{1,p.identity.Snapshot,p.params.Request.ProjectID,items}
 digest,err:=canonical.Hash(unsigned);if err!=nil{return SnapshotDocument{},err}
 snapshot:=ArtifactSnapshot{1,p.identity.Snapshot,digest,p.params.Request.ProjectID,items}
 body,err:=canonical.Marshal(snapshot);if err!=nil{return SnapshotDocument{},err}
 bodySHA,err:=canonical.Hash(snapshot);if err!=nil{return SnapshotDocument{},err}
 ref:=v2input.Ref{ArtifactID:uuid.NewSHA1(p.identity.Workflow,[]byte("interchange:artifact-snapshot-document:1")),SHA256:bodySHA}
 return SnapshotDocument{Snapshot:snapshot,Ref:ref,Body:body},nil
}

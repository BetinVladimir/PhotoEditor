package delivery

import (
 "bytes"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

type MetadataInventoryEntry struct {
 Ordinal int `json:"ordinal"`
 Binding v2input.SessionVersionBinding `json:"binding"`
 Metadata v2input.Ref `json:"metadata_source"`
 PublicationSHA256 string `json:"publication_sha256"`
 FrozenSHA256 string `json:"frozen_sha256"`
}
type metadataInventoryEnvelope struct {
 SchemaVersion int `json:"schema_version"`
 ParentRun uuid.UUID `json:"parent_run_id"`
 Snapshot v2input.Ref `json:"snapshot"`
 SnapshotDigest string `json:"snapshot_digest"`
 Policy string `json:"policy"`
 ApprovedFields []string `json:"approved_fields"`
 Items []MetadataInventoryEntry `json:"items"`
}
// CompileMetadataInventory binds exactly one trusted publication to every
// snapshot version. Publication authority/live ready state still require SQL.
// Callers must supply the authenticated external scope for each publication.
func CompileMetadataInventory(parent uuid.UUID,snapshotRef v2input.Ref,snapshotRaw []byte,policy string,approved []string,
 scopes []MetadataSourcePublicationScope,publications []MetadataSourcePublication)([]byte,string,error){
 invalid:=func()([]byte,string,error){return nil,"",v2input.ErrInvalid}
 if parent==uuid.Nil||snapshotRef.ArtifactID==uuid.Nil||len(snapshotRaw)>8<<20||digest(snapshotRaw)!=snapshotRef.SHA256||policy=="strip"{return invalid()}
 fields,err:=normalizeMetadataApproval(policy,approved);if err!=nil{return invalid()}
 var snapshot v2input.SessionInputSnapshot
 if json.Unmarshal(snapshotRaw,&snapshot)!=nil{return invalid()}
 _,exact,err:=v2input.CompilePinnedSessionInputSnapshot(snapshot.SnapshotID,snapshot.Items)
 if err!=nil||!bytes.Equal(exact,snapshotRaw)||len(scopes)!=len(snapshot.Items)||len(publications)!=len(snapshot.Items){return invalid()}
 entries:=make([]MetadataInventoryEntry,0,len(snapshot.Items));seen:=map[uuid.UUID]bool{}
 var principal MetadataSourcePublicationScope
 for n,item:=range snapshot.Items{
  scope:=scopes[n];plan:=publications[n]
  if scope.Binding!=item.Binding{return invalid()}
  if n==0{principal=scope}else if scope.Tenant!=principal.Tenant||scope.Project!=principal.Project||scope.Actor!=principal.Actor{return invalid()}
  id,err:=MetadataSourceIdentity(parent,item.Binding,policy,fields)
  if err!=nil||plan.Ref().ArtifactID!=id||seen[id]{return invalid()};seen[id]=true
  owned,err:=RestoreMetadataSourcePublication(plan.JCS(),plan.SHA256(),scope,plan.Ref(),plan.SourceJCS(),policy,fields)
  if err!=nil{return invalid()}
  entries=append(entries,MetadataInventoryEntry{Ordinal:n,Binding:item.Binding,Metadata:owned.Ref(),PublicationSHA256:owned.SHA256(),FrozenSHA256:digest(owned.FrozenJCS())})
 }
 raw,err:=canonical.Marshal(metadataInventoryEnvelope{SchemaVersion:1,ParentRun:parent,Snapshot:snapshotRef,SnapshotDigest:snapshot.SnapshotDigest,Policy:policy,ApprovedFields:append([]string{},fields...),Items:entries})
 if err!=nil||len(raw)>8<<20{return invalid()};return raw,digest(raw),nil
}

// FrozenMetadataInventory can only be obtained by external reconstruction. It
// owns canonical bytes and exposes entries by value; it proves no SQL readiness.
type FrozenMetadataInventory struct {body []byte;pin string;value metadataInventoryEnvelope}
func(i FrozenMetadataInventory)JCS()[]byte{return append([]byte(nil),i.body...)}
func(i FrozenMetadataInventory)SHA256()string{return i.pin}
func(i FrozenMetadataInventory)Entry(ordinal int,binding v2input.SessionVersionBinding)(MetadataInventoryEntry,error){
 if i.pin==""||ordinal<0||ordinal>=len(i.value.Items){return MetadataInventoryEntry{},v2input.ErrInvalid}
 entry:=i.value.Items[ordinal]
 if entry.Ordinal!=ordinal||entry.Binding!=binding{return MetadataInventoryEntry{},v2input.ErrInvalid}
 return entry,nil
}
func RestoreMetadataInventory(raw []byte,pin string,parent uuid.UUID,snapshotRef v2input.Ref,snapshotRaw []byte,policy string,approved []string,
 scopes []MetadataSourcePublicationScope,publications []MetadataSourcePublication)(FrozenMetadataInventory,error){
 var zero FrozenMetadataInventory
 if len(raw)>8<<20||!shaPattern.MatchString(pin)||digest(raw)!=pin{return zero,v2input.ErrInvalid}
 exact,exactPin,err:=CompileMetadataInventory(parent,snapshotRef,snapshotRaw,policy,approved,scopes,publications)
 if err!=nil||exactPin!=pin||!bytes.Equal(raw,exact){return zero,v2input.ErrInvalid}
 var value metadataInventoryEnvelope
 if json.Unmarshal(exact,&value)!=nil{return zero,v2input.ErrInvalid}
 return FrozenMetadataInventory{body:exact,pin:pin,value:value},nil
}

// CompileChildConfig uses only a publication already pinned in the restored
// parent inventory. Current slot and publication readiness remain SQL fences.
func(i FrozenMetadataInventory)CompileChildConfig(project uuid.UUID,request FrozenExportRequest,exportID uuid.UUID,
 publication MetadataSourcePublication)([]byte,string,error){
 invalid:=func()([]byte,string,error){return nil,"",v2input.ErrInvalid}
 if i.pin==""||request.ParentRunID!=i.value.ParentRun||request.SnapshotDigest!=i.value.SnapshotDigest{return invalid()}
 entry,err:=i.Entry(request.Ordinal,request.Binding)
 if err!=nil||entry.Metadata!=publication.Ref()||entry.PublicationSHA256!=publication.SHA256()||entry.FrozenSHA256!=digest(publication.FrozenJCS()){return invalid()}
 return CompileMetadataChildConfig(project,request,exportID,publication,i.value.Policy,append([]string{},i.value.ApprovedFields...))
}

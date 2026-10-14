package delivery

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

// MetadataSourcePublicationService must be installed trusted API code, never
// an HTTP-supplied publication. Service implements this interface.
type MetadataSourcePublicationService interface {
 PrepareAndPublishMetadataSource(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,
 v2input.SessionVersionBinding,objectstore.Object,string,[]string,MetadataSourceAPIDependencies)(MetadataSourcePublication,error)
}
const metadataInventoryPreparationBytes = 64<<20

// PrepareMetadataInventory resumes deterministic per-version publications after
// draft run creation. Prior successful publications remain on failure and exact
// retry uses their existing identities. No incomplete inventory is returned.
// All work is sequential and no transaction spans extraction or object IO.
func PrepareMetadataInventory(ctx context.Context,service MetadataSourcePublicationService,who auth.Identity,
 tenant,project,parent uuid.UUID,snapshotRef v2input.Ref,snapshotRaw []byte,workingImages []objectstore.Object,
 policy string,approved []string,deps MetadataSourceAPIDependencies)(FrozenMetadataInventory,error){
 var zero FrozenMetadataInventory
 if err:=ctx.Err();err!=nil{return zero,err}
 if service==nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||tenant==uuid.Nil||project==uuid.Nil||parent==uuid.Nil||
 snapshotRef.ArtifactID==uuid.Nil||len(snapshotRaw)>8<<20||digest(snapshotRaw)!=snapshotRef.SHA256||policy=="strip"{return zero,v2input.ErrInvalid}
 fields,err:=normalizeMetadataApproval(policy,approved);if err!=nil{return zero,err}
 var snapshot v2input.SessionInputSnapshot
 if json.Unmarshal(snapshotRaw,&snapshot)!=nil{return zero,v2input.ErrInvalid}
 _,exact,err:=v2input.CompilePinnedSessionInputSnapshot(snapshot.SnapshotID,snapshot.Items)
 if err!=nil||!bytes.Equal(exact,snapshotRaw)||len(workingImages)!=len(snapshot.Items){return zero,v2input.ErrInvalid}
 images:=append([]objectstore.Object(nil),workingImages...)
 // Validate the complete input list before creating the first publication.
 for n,item:=range snapshot.Items{
  image:=images[n]
  if image.SHA256!=item.WorkingImage.SHA256||image.Key==""||image.VersionID==""||image.VersionID=="null"||image.Bytes<1{return zero,v2input.ErrInvalid}
 }
 scopes:=make([]MetadataSourcePublicationScope,0,len(images));plans:=make([]MetadataSourcePublication,0,len(images));retained:=0
 for n,item:=range snapshot.Items{
  if err=ctx.Err();err!=nil{return zero,err}
  plan,prepareErr:=service.PrepareAndPublishMetadataSource(ctx,who,tenant,project,parent,item.Binding,images[n],policy,append([]string{},fields...),deps)
  if prepareErr!=nil{return zero,prepareErr}
  var envelope metadataSourcePublicationEnvelope
  if json.Unmarshal(plan.JCS(),&envelope)!=nil{return zero,v2input.ErrInvalid}
  scope:=envelope.Scope
  if scope.Tenant!=tenant||scope.Project!=project||scope.Actor!=who.UserID||scope.Session==uuid.Nil||scope.Binding!=item.Binding{return zero,v2input.ErrInvalid}
  retained+=len(plan.body)+len(plan.source)+len(plan.frozen)
  if retained>metadataInventoryPreparationBytes{return zero,v2input.ErrInvalid}
  scopes=append(scopes,scope);plans=append(plans,plan)
 }
 if err=ctx.Err();err!=nil{return zero,err}
 raw,pin,err:=CompileMetadataInventory(parent,snapshotRef,exact,policy,fields,scopes,plans)
 if err!=nil{return zero,err}
 return RestoreMetadataInventory(raw,pin,parent,snapshotRef,exact,policy,fields,scopes,plans)
}

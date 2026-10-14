package delivery

import (
 "context"
 "encoding/json"
 "sort"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
// MetadataOriginalResolver verifies canonical WorkingImage/Canvas bytes before
// resolving original lineage through the current API principal/session ACL.
type MetadataOriginalResolver interface {
 Load(context.Context,uuid.UUID,uuid.UUID,uuid.UUID,uuid.UUID,v2input.SessionVersionBinding,objectstore.Object)(v2input.LensOriginalSource,error)
}
// MetadataSourcePreparer is a trusted installed extraction service. Never bind
// this interface to client-authored JSON or treat a self-consistent receipt as IO.
type MetadataSourcePreparer interface {
 Prepare(context.Context,MetadataSourcePublicationScope,uuid.UUID,string,[]string)(MetadataSourcePublication,error)
}
type MetadataSourceAPIDependencies struct {
 Original MetadataOriginalResolver
 Preparer MetadataSourcePreparer
 Objects artifactjson.Objects
 Bucket string
}
func MetadataSourceIdentity(run uuid.UUID,binding v2input.SessionVersionBinding,policy string,approved []string)(uuid.UUID,error){
 if run==uuid.Nil||binding.AssetVersionID==uuid.Nil||binding.Source.ArtifactID==uuid.Nil||!shaPattern.MatchString(binding.Source.SHA256)||
 !shaPattern.MatchString(binding.CanvasHash)||policy!="copyright"&&policy!="approved_exif"{return uuid.Nil,v2input.ErrInvalid}
 if _,err:=ProjectDeliveryMetadata(MetadataDocument{},policy,approved);err!=nil{return uuid.Nil,err}
 if policy!="approved_exif"&&len(approved)>0{return uuid.Nil,v2input.ErrInvalid}
 fields:=append([]string{},approved...);sort.Strings(fields)
 raw,err:=canonical.Marshal(struct{Binding v2input.SessionVersionBinding `json:"binding"`;Policy string `json:"policy"`;Approved []string `json:"approved_fields"`}{binding,policy,fields})
 if err!=nil{return uuid.Nil,err}
 return uuid.NewSHA1(run,[]byte("delivery:metadata:source:"+digest(raw))),nil
}

// PrepareAndPublishMetadataSource runs after draft delivery run creation and
// before graph admission. It does not accept normalized source bytes from HTTP,
// activate the run or mark public CreateDelivery successful.
func(s Service)PrepareAndPublishMetadataSource(ctx context.Context,who auth.Identity,tenant,project,run uuid.UUID,
 binding v2input.SessionVersionBinding,workingImage objectstore.Object,policy string,approved []string,
 deps MetadataSourceAPIDependencies)(MetadataSourcePublication,error){
 var zero MetadataSourcePublication
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.DB==nil||tenant==uuid.Nil||project==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||
 deps.Original==nil||deps.Preparer==nil||deps.Objects==nil||len(deps.Bucket)<3||len(deps.Bucket)>63{return zero,v2input.ErrInvalid}
 fields:=append([]string{},approved...)
 id,err:=MetadataSourceIdentity(run,binding,policy,fields);if err!=nil{return zero,err}
 original,err:=deps.Original.Load(ctx,tenant,project,who.UserID,who.SessionID,binding,workingImage)
 if err!=nil{return zero,err}
 scope:=MetadataSourcePublicationScope{Tenant:tenant,Project:project,Actor:who.UserID,Session:who.SessionID,Binding:binding,
 Original:original.Ref,OriginalBytes:original.Object.Bytes,OriginalMIME:original.Object.ContentType}
 // Validate resolved original and fixed metadata identity before extraction.
 empty,err:=canonical.Marshal(MetadataDocument{});if err!=nil{return zero,err}
 if _,err=CompileMetadataSourcePublication(scope,v2input.Ref{ArtifactID:id,SHA256:digest(empty)},empty,policy,fields);err!=nil{return zero,err}
 if err=ctx.Err();err!=nil{return zero,err}
 owned,saved,err:=ReadMetadataSourceRetry(ctx,metadataSourceQueries{db:s.DB,tenant:tenant},run,scope,policy,fields)
 if err!=nil{return zero,err}
 if !saved{
  publication,prepareErr:=deps.Preparer.Prepare(ctx,scope,id,policy,append([]string{},fields...));if prepareErr!=nil{return zero,prepareErr}
  if publication.Ref().ArtifactID!=id{return zero,v2input.ErrInvalid}
  owned,err=RestoreMetadataSourcePublication(publication.JCS(),publication.SHA256(),scope,publication.Ref(),publication.SourceJCS(),policy,fields)
  if err!=nil{return zero,err}
 }
 var envelope metadataSourcePublicationEnvelope
 if json.Unmarshal(owned.JCS(),&envelope)!=nil{return zero,v2input.ErrInvalid}
 repository,err:=NewMetadataSourceRepository(s.DB,envelope.Scope,run,owned,policy,fields,deps.Bucket);if err!=nil{return zero,err}
 repository,err=repository.WithCurrentSession(who.SessionID);if err!=nil{return zero,err}
 if _,err=PublishMetadataSource(ctx,repository,deps.Objects);err!=nil{return zero,err}
 return owned,ctx.Err()
}

// NewMetadataOriginalResolver installs the existing physical WorkingImage
// verifier with tenant-scoped API queries. No SQL transaction spans object IO.
func NewMetadataOriginalResolver(db MetadataTransactionBeginner,tenant uuid.UUID,bucket string,
 objects v2input.SessionAnalysisObjectReader)(v2input.SQLDeliveryMetadataOriginal,error){
 if db==nil||tenant==uuid.Nil||len(bucket)<3||len(bucket)>63||objects==nil{return v2input.SQLDeliveryMetadataOriginal{},v2input.ErrInvalid}
 return v2input.SQLDeliveryMetadataOriginal{DB:metadataSourceQueries{db:db,tenant:tenant},Bucket:bucket,Objects:objects},nil
}

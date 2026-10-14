package workflow

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

type replacementDetectionInputCatalog struct{source ReplacementDetectionDocuments}
func(c replacementDetectionInputCatalog) resolve(ctx context.Context,role string,ref v2input.Ref)(objectstore.Object,error){
 scope,body,task,_,err:=c.source.Plan.PreparePublication(ctx);if err!=nil{return objectstore.Object{},err}
 if _,err=c.source.validate(ctx,scope,task.Params.SHA256,int64(len(body)));err!=nil{return objectstore.Object{},err}
 var object objectstore.Object
 err=c.source.transaction(ctx,scope,func(ctx context.Context,tx pgx.Tx,proof []byte)error{
  var raw []byte
  if err:=tx.QueryRow(ctx,"SELECT photo.v29_replacement_detector_input_object($1,$2,$3,$4,$5,$6,$7::jsonb)",scope.Tenant,scope.Run,scope.Owner,role,ref.ArtifactID,ref.SHA256,string(proof)).Scan(&raw);err!=nil{return err}
  if len(raw)<2||len(raw)>16384{return v2input.ErrExpired}
  normal,err:=canonical.Bytes(raw);if err!=nil{return err}
  var wire perspectiveLinesObjectWitness
  if json.Unmarshal(normal,&wire)!=nil{return v2input.ErrInvalid}
  exact,err:=canonical.Marshal(wire);if err!=nil||!bytes.Equal(normal,exact)||wire.Artifact!=ref.ArtifactID||wire.SHA!=ref.SHA256||wire.Bucket!=c.source.Bucket||wire.Bytes<1||wire.Key==""||len(wire.Key)>1024||wire.Version==""||wire.Version=="null"||len(wire.Version)>1024{return v2input.ErrInvalid}
  object=objectstore.Object{Key:wire.Key,VersionID:wire.Version,SHA256:wire.SHA,Bytes:wire.Bytes,ContentType:wire.Media};return nil
 });if err!=nil{return objectstore.Object{},err};return object,nil
}
func(c replacementDetectionInputCatalog) identity(ctx context.Context,tenant,project,snapshot uuid.UUID,who auth.Identity)error{
 if _,_,_,_,err:=c.source.Plan.PreparePublication(ctx);err!=nil{return err}
 d,err:=decodeReplacementDetectionDescriptor(c.source.Plan.Descriptor());if err!=nil{return err}
 if tenant!=d.Tenant||project!=d.Project||snapshot!=c.source.Snapshot||snapshot==uuid.Nil||who.UserID!=d.Actor||who.SessionID!=d.Session{return v2input.ErrInvalid};return nil
}
func(c replacementDetectionInputCatalog) ResolveSource(ctx context.Context,scope PerspectiveLinesScope,snapshot uuid.UUID,who auth.Identity)(objectstore.Object,error){
 if err:=c.identity(ctx,scope.Tenant,scope.Project,snapshot,who);err!=nil{return objectstore.Object{},err}
 if scope!=c.source.Plan.detector.scope{return objectstore.Object{},v2input.ErrInvalid}
 return c.resolve(ctx,"WorkingImage",scope.Base.Source)
}
func(c replacementDetectionInputCatalog) ResolvePerspectiveSourceArray(ctx context.Context,who auth.Identity,tenant,project,snapshot uuid.UUID,params v2optical.PerspectiveParams,raw []byte,spec v2optical.PerspectiveSourceArraySpec)(objectstore.Object,error){
 if err:=c.identity(ctx,tenant,project,snapshot,who);err!=nil{return objectstore.Object{},err}
 if params.Base!=c.source.Plan.detector.scope.Base||!bytes.Equal(raw,c.source.Plan.detector.image)||(spec.Role!="WorkingPixels"&&spec.Role!="WorkingAlpha"){return objectstore.Object{},v2input.ErrInvalid}
 specs,err:=v2optical.PerspectiveSourceArrays(params,raw);if err!=nil{return objectstore.Object{},err}
 found:=false;for _,known:=range specs{if known==spec{found=true}}
 if !found{return objectstore.Object{},v2input.ErrInvalid}
 return c.resolve(ctx,spec.Role,spec.Ref)
}
func(r ReplacementDetectionDocuments) VerifyInputObjects(ctx context.Context,objects v2input.SessionAnalysisObjectReader)(VerifiedReplacementDetectionInputs,error){
 catalog:=replacementDetectionInputCatalog{source:r}
 return r.Plan.VerifyInputs(ctx,r.Snapshot,catalog,catalog,objects)
}

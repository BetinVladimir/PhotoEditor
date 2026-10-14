package provenance

import (
 "context"
 "errors"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

// Only no-row permits the negative adopted-child fallback; authority errors
// from direct/render producers are never bypassed.
type SigningImageProducerWithNegative struct{Existing,Negative SigningImageProducer}
func(s SigningImageProducerWithNegative)ResolveSigningImageProducer(ctx context.Context,who auth.Identity,ready ReadySigningExport,pin v2input.Ref)(uuid.UUID,error){
 if err:=ctx.Err();err!=nil{return uuid.Nil,err}
 if s.Existing==nil||s.Negative==nil{return uuid.Nil,ErrInvalid}
 id,err:=s.Existing.ResolveSigningImageProducer(ctx,who,ready,pin)
 if !errors.Is(err,pgx.ErrNoRows){return id,err}
 return s.Negative.ResolveSigningImageProducer(ctx,who,ready,pin)
}

type NegativeSigningTaskRouter struct{Existing,Negative SigningTaskLineageResolver}
func(s NegativeSigningTaskRouter)ResolveSigningTaskLineage(ctx context.Context,who auth.Identity,ready ReadySigningExport,task CanonicalSigningTask)(ResolvedSigningTask,error){
 if err:=ctx.Err();err!=nil{return ResolvedSigningTask{},err}
 resolver:=s.Existing;if task.Operation=="optical.negative"{resolver=s.Negative}
 if resolver==nil{return ResolvedSigningTask{},ErrInvalid}
 return resolver.ResolveSigningTaskLineage(ctx,who,ready,task)
}

// All locks are independently installed immutable historical policy revisions.
// Construction performs no SQL/object IO or native signing qualification.
func NewSQLSigningNegativeEvidence(db *pgxpool.Pool,bucket string,tasks SigningTaskCatalog,objects SigningObjects,policies *SigningNegativePolicyRegistry)(BoundSigningNegativeEvidenceReader,error){
 var zero BoundSigningNegativeEvidenceReader
 if db==nil||tasks==nil||objects==nil||policies==nil||len(policies.policies)==0||len(bucket)<3||!text(bucket,63){return zero,ErrInvalid}
 adoption:=SQLSigningNegativeAdoptionReader{DB:db,Tasks:tasks}
 documents:=BoundSigningTaskDocumentReader{Catalog:SQLSigningTaskDocumentCatalog{DB:db,Bucket:bucket,Tasks:tasks},Objects:objects}
 return BoundSigningNegativeEvidenceReader{
  Adoption:adoption,Documents:documents,
  Counter:BoundSigningNegativeMaskCounter{Catalog:SQLSigningNegativeMaskCatalog{DB:db,Bucket:bucket,Adoption:adoption,Documents:documents},Objects:objects},
  SourceArrays:BoundSigningNegativeSourceArraysVerifier{Catalog:SQLSigningNegativeSourceArrayCatalog{DB:db,Bucket:bucket,Adoption:adoption,Documents:documents},Objects:objects},
  Pixels:BoundSigningNegativePixelsVerifier{Catalog:SQLSigningNegativePixelsCatalog{DB:db,Bucket:bucket,Adoption:adoption,Documents:documents},Objects:objects},
  Qualification:BoundSigningNegativeQualificationContent{Historical:SQLSigningNegativeQualificationReader{DB:db,Adoption:adoption,Documents:documents},Documents:documents,Policies:policies},
  Reviews:SQLSigningNegativeReviewReader{DB:db,Adoption:adoption,Documents:documents},
 },nil
}

func NewSQLNegativeRenderPresentationSigningExecution(db *pgxpool.Pool,bucket string,objects SigningObjects,workICCSHA string,policies *SigningNegativePolicyRegistry)(SigningExecutionSource,error){
 presentation,err:=NewSQLPresentationSigningExecution(db,bucket,objects,workICCSHA);if err!=nil{return nil,err}
 bound,ok:=presentation.(BoundSigningExecutionSource);if !ok{return nil,ErrInvalid}
 negative,err:=NewSQLSigningNegativeEvidence(db,bucket,bound.Tasks,objects,policies);if err!=nil{return nil,err}
 reviewed,err:=NewSQLSigningRenderReviewedCausalSource(db,bucket,bound.Tasks,objects);if err!=nil{return nil,err}
 base,ok:=bound.Claims.(PresentationSigningTaskResolver);if !ok{return nil,ErrInvalid}
 producers:=SigningImageProducerWithNegative{Existing:base.Producers,Negative:BoundSigningNegativeImageProducer{Catalog:SQLSigningNegativeImageProducerCatalog{DB:db,Tasks:bound.Tasks},Adoption:negative.Adoption,Documents:negative.Documents}}
 base.Producers=producers
 reviewed.Causal.Producers=producers
 render:=RenderSigningTaskResolver{Reviewed:reviewed,Dependencies:BoundSigningRenderDependencyResolver{Causal:reviewed.Causal}}
 causal:=BoundSigningNegativeCausalSource{Evidence:negative,Producers:producers,Documents:negative.Documents}
 bound.Claims=NegativeSigningTaskRouter{Existing:RenderPresentationSigningTaskResolver{Presentation:base,Render:render},Negative:NegativeSigningTaskResolver{Causal:causal}}
 return bound,nil
}

func NewSQLNegativeRenderPresentationSigningExportSource(db *pgxpool.Pool,bucket string,reads SigningObjects,writes artifactjson.Objects,workICCSHA string,policies *SigningNegativePolicyRegistry)(SigningExportSource,error){
 if writes==nil{return nil,ErrInvalid}
 execution,err:=NewSQLNegativeRenderPresentationSigningExecution(db,bucket,reads,workICCSHA,policies);if err!=nil{return nil,err}
 exports:=SQLSigningExportCatalog{DB:db,Bucket:bucket}
 publisher:=ImmutableSigningLineagePublisher{Catalog:exports,Store:SQLSigningLineageStore{DB:db,Bucket:bucket},Objects:writes}
 return BoundSigningExportSource{Catalog:exports,Lineages:CompiledExportLineageSource{Execution:execution,Documents:publisher}},nil
}

func NewSQLNegativeRenderPresentationSigningService(config PresentationSigningServiceInstallation,entries []InstalledSigningNegativePolicy)(*SQLSignExportService,error){
 policies,err:=NewSigningNegativePolicyRegistry(entries);if err!=nil{return nil,err}
 return newSQLNegativeSigningService(config,policies)
}

func newSQLNegativeSigningService(config PresentationSigningServiceInstallation,policies *SigningNegativePolicyRegistry)(*SQLSignExportService,error){
 if policies==nil||len(policies.policies)==0{return nil,ErrInvalid}
 return newSQLSigningService(config,func(db *pgxpool.Pool,bucket string,reads SigningObjects,writes artifactjson.Objects,workICCSHA string)(SigningExportSource,error){
  return NewSQLNegativeRenderPresentationSigningExportSource(db,bucket,reads,writes,workICCSHA,policies)
 })
}

var _ SigningTaskLineageResolver=NegativeSigningTaskRouter{}
var _ SigningImageProducer=SigningImageProducerWithNegative{}

// Deployment path and digest come only from authenticated startup configuration.
func NewSQLNegativeSigningServiceFromPolicyFile(ctx context.Context,config PresentationSigningServiceInstallation,path,expectedSHA string)(*SQLSignExportService,error){
 policies,err:=LoadSigningNegativePolicyDocument(ctx,path,expectedSHA);if err!=nil{return nil,err}
 if err:=ctx.Err();err!=nil{return nil,err}
 return newSQLNegativeSigningService(config,policies)
}

package interchange

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

type ExportExecutionObjects interface{ImportDocumentObjects;artifactjson.Objects}
type SQLExportExecutionInstaller struct{
 API,Scheduler *pgxpool.Pool
 Objects ExportExecutionObjects
 Bucket string
 Dependencies ExportTypedDocumentValidator
 DependencyFactory ExportDocumentValidatorFactory
 Binaries ExportBinaryQualifier
 Native *ExportNativeResolverInstallation
 Qualification ExportResourceQualifier
 Resources ExportTaskResources
 MaxInputBytes int64
}
// A candidate envelope is selected from trusted evidence; full qualification
// still requires the freshly resolved inventory before task materialization.
type ExportResourceSelector interface{
 SelectExportResources(context.Context,CreateIntent,FrozenExportDocuments)(ExportTaskResources,error)
}
// Construction performs no IO; every adapter receives owned frozen request data.
func(s SQLExportExecutionInstaller)InstallExportExecution(ctx context.Context,who auth.Identity,tid uuid.UUID,request CreateRequest,key string,frozen FrozenExportDocuments)(ExportExecutionInstallation,error){
 var zero ExportExecutionInstallation
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.API==nil||s.Scheduler==nil||s.API==s.Scheduler||s.Objects==nil||len(s.Bucket)<3||len(s.Bucket)>63||(s.Dependencies==nil&&s.DependencyFactory==nil)||(s.Dependencies!=nil&&s.DependencyFactory!=nil)||(s.Binaries==nil&&s.Native==nil)||(s.Binaries!=nil&&s.Native!=nil)||s.Qualification==nil{return zero,v2input.ErrInvalid}
 intent,err:=FreezeCreateIntent(tid,who,request,key);if err!=nil{return zero,err}
 checked,_,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return zero,err}
 resources:=s.Resources
 if selector,ok:=s.Qualification.(ExportResourceSelector);ok{
  resources,err=selector.SelectExportResources(ctx,copyExportIntent(intent),FrozenExportDocuments{body:checked.Body(),identity:checked.identity})
  if err!=nil{return zero,err}
 }
 catalog:=SQLExportDocumentCatalog{DB:s.API,Tenant:tid,Who:who,Request:copyExportIntent(intent).Request,Key:key,Bucket:s.Bucket,Frozen:FrozenExportDocuments{body:checked.Body(),identity:checked.identity}}
 dependencies:=s.Dependencies
 if s.DependencyFactory!=nil{
  owned:=FrozenExportDocuments{body:checked.Body(),identity:checked.identity}
  if scoped,ok:=s.DependencyFactory.(ExportScopedDocumentValidatorFactory);ok{
   dependencies,err=scoped.BuildScopedExportDocumentValidator(ctx,copyExportIntent(intent),owned,catalog,s.Objects)
  }else{dependencies,err=s.DependencyFactory.BuildExportDocumentValidator(ctx,copyExportIntent(intent),owned)}
  if err!=nil{return zero,err};if dependencies==nil{return zero,v2input.ErrInvalid}
 }
 var resolver ExportClosureNodeResolver
 if s.Native!=nil{
  if s.MaxInputBytes<1||s.MaxInputBytes>128<<30{return zero,v2input.ErrInvalid}
  validator,err:=NewExportScopedDocumentValidator(ctx,intent,checked,dependencies);if err!=nil{return zero,err}
  native:=*s.Native
  native.Catalog=catalog;native.Objects=s.Objects;native.MaxBytes=s.MaxInputBytes
  native.JSON=ExportJSONNodeResolver{Catalog:catalog,Objects:s.Objects,Validator:validator}
  if native.Scratch==nil||native.ImageProbe==nil||native.MaskProbe==nil||native.ArrayProbe==nil||!validImportRef(v2input.Ref{ArtifactID:intent.Identity.Task,SHA256:native.ImageRuntimeSHA})||!validImportRef(v2input.Ref{ArtifactID:intent.Identity.Task,SHA256:native.ArrayRuntimeSHA}){return zero,v2input.ErrInvalid}
  resolver=native
 }else{
  installed,err:=NewSQLExportResolver(ctx,catalog,s.Objects,dependencies,s.Binaries,s.MaxInputBytes);if err!=nil{return zero,err};resolver=installed
 }
 store:=SQLExportDocumentStore{DB:s.API,Tenant:tid,Who:who,Intent:copyExportIntent(intent),Key:key,Bucket:s.Bucket,Frozen:FrozenExportDocuments{body:checked.Body(),identity:checked.identity}}
 materializer:=SQLResolvedExportTaskMaterializer{SQL:SQLExportTaskMaterializer{DB:s.API,Tenant:tid,Who:who,Request:copyExportIntent(intent).Request,Key:key,Resources:resources},Resolver:resolver}
 return ExportExecutionInstallation{
  Resources:resources,
  Execution:ExportExecutionPreparation{
   Documents:ExportStoragePublisher{Tenant:tid,Store:store,Objects:s.Objects},Resolver:resolver,Materializer:materializer,Qualification:s.Qualification,
   TransportDocuments:artifactjson.Publisher{Repository:artifactjson.SQLRepository{DB:s.Scheduler,Bucket:s.Bucket},Objects:s.Objects},
   Registrar:SQLExportInputRegistrar{DB:s.Scheduler,Tenant:tid,Who:who,Request:copyExportIntent(intent).Request,Key:key},
  },
  Activation:ExportActivationPreparation{Resolver:resolver,Resources:s.Qualification},
 },nil
}

// The materializer resolves inventory after Plan/Params publication. It never
// trusts an input digest provided without the corresponding qualified closure.
type SQLResolvedExportTaskMaterializer struct{SQL SQLExportTaskMaterializer;Resolver ExportClosureNodeResolver}
func(s SQLResolvedExportTaskMaterializer)MaterializeExport(ctx context.Context,intent CreateIntent,frozen FrozenExportDocuments,task v2input.DraftTask)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.SQL.DB==nil||s.Resolver==nil{return v2input.ErrInvalid}
 _,compiled,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return err}
 inventory,err:=ResolveExportInputInventory(ctx,compiled,s.Resolver);if err!=nil{return err}
 if inventory.digest!=task.InputDigest{return v2input.ErrInvalid}
 materializer:=s.SQL;materializer.Inventory=inventory
 return materializer.MaterializeExport(ctx,intent,frozen,task)
}
var _ ExportExecutionInstaller=SQLExportExecutionInstaller{}

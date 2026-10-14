package interchange

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
type ImportReviewStyleQualifierFactory interface{
 BuildReviewStyleQualifier(context.Context,SQLImportApprovalCatalog,uuid.UUID,ImportDocumentObjects)(ImportStyleQualifier,error)
}
type NativeImportReviewStyleFactory struct{
 scratch *ExportArchiveScratch
 probe ExportNativeProbe
 models map[string]string
 runtime,icc string
}
func NewNativeImportReviewStyleFactory(scratch *ExportArchiveScratch,probe ExportNativeProbe,models map[string]string,runtime,icc string)(*NativeImportReviewStyleFactory,error){
 if scratch==nil||probe==nil||models==nil||len(models)>1024||!validImportRefForSHA(runtime)||!validImportRefForSHA(icc){return nil,v2input.ErrInvalid}
 owned:=map[string]string{}
 for key,value:=range models{if len(key)<1||len(key)>240||len(value)<1||len(value)>240{return nil,v2input.ErrInvalid};owned[key]=value}
 return &NativeImportReviewStyleFactory{scratch:scratch,probe:probe,models:owned,runtime:runtime,icc:icc},nil
}
type reviewStyleMasterCatalog struct{prepared SQLImportApprovalCatalog;project uuid.UUID}
func(c reviewStyleMasterCatalog)LookupImportDocument(ctx context.Context,entry v2input.Entry)(objectstore.Object,error){
 if entry.Contract!="WorkingImage"||entry.Ref!=c.prepared.Scope.Binding.Source{return objectstore.Object{},v2input.ErrInvalid}
 ordinary:=v2draft.SQLArtifactCatalog{DB:c.prepared.DB,Bucket:c.prepared.Bucket,ProjectID:c.project,AssetVersionID:c.prepared.Scope.Binding.AssetVersionID}
 return ordinary.Lookup(ctx,c.prepared.Who,c.prepared.Scope.Tenant,v2draft.LayerRef{Ref:entry.Ref,Contract:entry.Contract})
}
func(f *NativeImportReviewStyleFactory)BuildReviewStyleQualifier(ctx context.Context,prepared SQLImportApprovalCatalog,project uuid.UUID,objects ImportDocumentObjects)(ImportStyleQualifier,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if f==nil||project==uuid.Nil||objects==nil||prepared.DB==nil||prepared.Scope.Tenant==uuid.Nil||prepared.Scope.Run==uuid.Nil||prepared.Scope.Workflow==uuid.Nil||prepared.Scope.Entry==uuid.Nil||prepared.Scope.Binding.AssetVersionID==uuid.Nil||!validImportRef(prepared.Scope.Binding.Source){return nil,v2input.ErrInvalid}
 prepared.Parsed=append([]byte(nil),prepared.Parsed...)
 masters:=ImportObjectMasterReader{Catalog:reviewStyleMasterCatalog{prepared:prepared,project:project},Objects:objects}
 scope:=BoundImportStyleScope{WorkICCSHA:f.icc,Masters:masters,Targets:SQLImportReviewTargetAuthority{Catalog:prepared}}
 return NewNativeImportStyleQualifier(f.scratch,f.probe,f.models,f.runtime,scope)
}
var _ ImportReviewStyleQualifierFactory=(*NativeImportReviewStyleFactory)(nil)

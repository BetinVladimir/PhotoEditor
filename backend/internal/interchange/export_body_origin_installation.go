package interchange
import (
 "context"
 "photoeditor/backend/internal/v2input"
)

type ExportBodyOriginValidatorFactory interface {
 BuildExportBodyOriginValidator(context.Context,CreateIntent,FrozenExportDocuments,ExportDocumentCatalog,ImportDocumentObjects,ExportClosureNodeResolver,ExportClosureNodeResolver,ExportDenseWarpOccurrenceAuthority,ExportSeriesMaskPolicyAuthority,ExportTypedDocumentValidator)(*NativeExportBodyOriginValidator,error)
}

type SQLExportBodyOriginInstallation struct {
 Dependencies SQLExportBodyDependencyInstallation
 Fit SQLExportBodyFitInstallation
 Scratch *ExportArchiveScratch
 StyleProbe,MaskProbe,ArrayProbe ExportNativeProbe
 BodyRuntimeSHA,MaskArrayRuntimeSHA,SourceArrayRuntimeSHA string
}
func(s SQLExportBodyOriginInstallation)BuildExportBodyOriginValidator(ctx context.Context,intent CreateIntent,frozen FrozenExportDocuments,catalog ExportDocumentCatalog,objects ImportDocumentObjects,documents,images ExportClosureNodeResolver,warps ExportDenseWarpOccurrenceAuthority,masks ExportSeriesMaskPolicyAuthority,other ExportTypedDocumentValidator)(*NativeExportBodyOriginValidator,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 scoped,ok:=catalog.(SQLExportDocumentCatalog);if !ok||scoped.DB==nil||objects==nil||documents==nil||images==nil||warps==nil||masks==nil||other==nil||s.Scratch==nil||s.StyleProbe==nil||s.MaskProbe==nil||s.ArrayProbe==nil{return nil,v2input.ErrInvalid}
 actual,err:=FreezeCreateIntent(scoped.Tenant,scoped.Who,scoped.Request,scoped.Key);if err!=nil{return nil,err}
 if actual.RequestSHA!=intent.RequestSHA||actual.Identity!=intent.Identity{return nil,v2input.ErrInvalid}
 checked,_,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return nil,err}
 if string(scoped.Frozen.Body())!=string(checked.Body()){return nil,v2input.ErrInvalid}
 models,err:=exportBodyDependencyNativeModels(s.Dependencies.Configs);if err!=nil{return nil,err}
 // One native bundle cannot admit conflicting locks for the same model.
 for _,config:=range s.Fit.Features.Configs{
  var name string;switch config.Expected.Operation{case "pose.detect":name="keypointrcnn_resnet50_fpn";case "face.landmarks":name="fan2d";default:return nil,v2input.ErrInvalid}
  if models[name]!=config.Expected.ModelLockSHA{return nil,v2input.ErrInvalid}
 }
 models["lraspp_mobilenet_v3_large"]=s.Fit.Person.Expected.ModelLockSHA
 for _,sha:=range []string{s.BodyRuntimeSHA,s.MaskArrayRuntimeSHA,s.SourceArrayRuntimeSHA}{if !validImportRef(v2input.Ref{ArtifactID:s.Fit.Person.Report.ArtifactID,SHA256:sha}){return nil,v2input.ErrInvalid}}
 images=ExportPhysicalSourceResolver{JSON:images,Catalog:scoped,Objects:objects,Working:scoped,Warps:warps,Scratch:s.Scratch,Probe:s.ArrayProbe,RuntimeSHA:s.SourceArrayRuntimeSHA}
 personPolicy,err:=s.Fit.Person.BuildExportStatsPersonPolicy(ctx,intent,checked,scoped,objects);if err!=nil{return nil,err}
 personSource:=QualifiedExportBodyPersonSource{Origin:scoped,Documents:ExportStatsPersonDocumentReader{Catalog:scoped,Objects:objects},Policy:personPolicy}
 person:=NativeExportBodyPersonQualifier{Source:personSource,Objects:objects,Scratch:s.Scratch,Probe:s.MaskProbe,RuntimeSHA:s.MaskArrayRuntimeSHA}
 dependencies,err:=s.Dependencies.BuildExportBodyDependencyQualifier(ctx,intent,checked,scoped,objects);if err!=nil{return nil,err}
 fit,err:=s.Fit.BuildExportBodyFitQualifier(ctx,intent,checked,scoped,objects,documents,images,warps,masks);if err!=nil{return nil,err}
 policy:=BoundExportBodyOriginPolicy{Source:scoped,Images:images,Warps:warps,Person:person,Dependencies:dependencies,FitInputs:fit}
 q,err:=NewNativeExportBodyOriginValidator(ExportBodyOriginReader{Source:scoped,Documents:documents},policy,other,s.Scratch,s.StyleProbe,models,s.BodyRuntimeSHA);if err!=nil{return nil,err}
 if err:=ctx.Err();err!=nil{return nil,err};return q,nil
}

var _ ExportBodyOriginValidatorFactory=SQLExportBodyOriginInstallation{}

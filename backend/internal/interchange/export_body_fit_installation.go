package interchange
import (
 "context"
 "photoeditor/backend/internal/v2input"
)

type SQLExportBodyFitInstallation struct {
 Alignment SQLExportAlignmentInstallation
 Features SQLExportBodyFitFeatureInstallation
 Person SQLExportStatsPersonPolicyInstallation
 Scratch *ExportArchiveScratch
 StyleProbe,MaskProbe ExportNativeProbe
 FitRuntimeSHA,MaskArrayRuntimeSHA string
}
func(s SQLExportBodyFitInstallation)BuildExportBodyFitQualifier(ctx context.Context,intent CreateIntent,frozen FrozenExportDocuments,catalog ExportDocumentCatalog,objects ImportDocumentObjects,documents,images ExportClosureNodeResolver,warps ExportDenseWarpOccurrenceAuthority,masks ExportSeriesMaskPolicyAuthority)(*NativeExportBodyFitQualifier,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 scoped,ok:=catalog.(SQLExportDocumentCatalog);if !ok||scoped.DB==nil||objects==nil||documents==nil||images==nil||warps==nil||masks==nil||s.Scratch==nil||s.StyleProbe==nil||s.MaskProbe==nil{return nil,v2input.ErrInvalid}
 actual,err:=FreezeCreateIntent(scoped.Tenant,scoped.Who,scoped.Request,scoped.Key);if err!=nil{return nil,err}
 if actual.RequestSHA!=intent.RequestSHA||actual.Identity!=intent.Identity{return nil,v2input.ErrInvalid}
 checked,_,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return nil,err}
 if string(scoped.Frozen.Body())!=string(checked.Body()){return nil,v2input.ErrInvalid}
 for _,sha:=range []string{s.FitRuntimeSHA,s.MaskArrayRuntimeSHA}{if !validImportRef(v2input.Ref{ArtifactID:s.Person.Report.ArtifactID,SHA256:sha}){return nil,v2input.ErrInvalid}}
 personPolicy,err:=s.Person.BuildExportStatsPersonPolicy(ctx,intent,checked,scoped,objects);if err!=nil{return nil,err}
 alignment,err:=s.Alignment.BuildExportAlignmentQualifier(ctx,intent,checked,scoped,objects,images,warps,masks);if err!=nil{return nil,err}
 features,err:=s.Features.BuildExportBodyFitFeatureQualifier(ctx,intent,checked,scoped,objects,documents,images,warps);if err!=nil{return nil,err}
 persons:=NativeExportBodyFitPairedPersons{Inventory:scoped,Origin:scoped,Images:images,Warps:warps,Documents:ExportStatsPersonDocumentReader{Catalog:scoped,Objects:objects},Policy:personPolicy,Objects:objects,Scratch:s.Scratch,Probe:s.MaskProbe,RuntimeSHA:s.MaskArrayRuntimeSHA}
 reader:=ExportBodyFitReader{Inventory:scoped,Documents:documents}
 causal:=BoundExportBodyFitCausal{Reader:reader,Documents:documents,Alignment:alignment,Features:features,Persons:persons}
 models:=map[string]string{"lraspp_mobilenet_v3_large":s.Person.Expected.ModelLockSHA}
 for _,config:=range s.Features.Configs{switch config.Expected.Operation{case "pose.detect":models["keypointrcnn_resnet50_fpn"]=config.Expected.ModelLockSHA;case "face.landmarks":models["fan2d"]=config.Expected.ModelLockSHA;default:return nil,v2input.ErrInvalid}}
 q,err:=NewNativeExportBodyFitQualifier(reader,causal,s.Scratch,s.StyleProbe,models,s.FitRuntimeSHA);if err!=nil{return nil,err}
 if err:=ctx.Err();err!=nil{return nil,err};return q,nil
}

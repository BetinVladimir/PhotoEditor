package interchange
import (
 "context"
 "photoeditor/backend/internal/v2input"
)
type SQLExportDetectorInstallation struct{
 Policy ExportDetectorPolicyDeployment
 Scratch *ExportArchiveScratch
 Probe ExportNativeProbe
 RuntimeSHA string
}
// Documents/images/warps are trusted adapters built for this exact frozen
// request; images must include physical validation rather than metadata only.
func(s SQLExportDetectorInstallation)BuildExportDetectorQualifier(ctx context.Context,intent CreateIntent,frozen FrozenExportDocuments,catalog ExportDocumentCatalog,objects ImportDocumentObjects,documents,images ExportClosureNodeResolver,warps ExportDenseWarpOccurrenceAuthority)(*QualifiedExportBodyFitFaceBoxes,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 scoped,ok:=catalog.(SQLExportDocumentCatalog);if !ok||scoped.DB==nil||objects==nil||documents==nil||images==nil||warps==nil||s.Scratch==nil||s.Probe==nil{return nil,v2input.ErrInvalid}
 actual,err:=FreezeCreateIntent(scoped.Tenant,scoped.Who,scoped.Request,scoped.Key);if err!=nil{return nil,err}
 if actual.RequestSHA!=intent.RequestSHA||actual.Identity!=intent.Identity{return nil,v2input.ErrInvalid}
 checked,_,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());if err!=nil{return nil,err}
 if string(scoped.Frozen.Body())!=string(checked.Body()){return nil,v2input.ErrInvalid}
 authority,err:=NewQualifiedExportBodyFitFaceBoxes(ExportBodyFitDetectorParamsReader{Source:scoped,Objects:objects},scoped,documents,images,warps,s.Policy.Report,s.Policy.Expected,s.Scratch,s.Probe,s.RuntimeSHA);if err!=nil{return nil,err}
 if err:=validateExportAuthorityFunctions(ctx,scoped.DB,[]string{"photo.v30_face_detector_qualification_object(uuid,uuid,uuid,uuid,jsonb)"});err!=nil{return nil,err}
 if err:=ctx.Err();err!=nil{return nil,err};return authority,nil
}

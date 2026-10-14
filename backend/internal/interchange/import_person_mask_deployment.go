package interchange
import (
 "context"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
type importPersonMaskDeploymentWire struct{
 Schema string `json:"schema_version"`
 Report v2input.Ref `json:"qualification_report"`
 Locks v2qualification.DeploymentLocks `json:"locks"`
}
func DecodeImportPersonMaskQualification(ctx context.Context,raw []byte,pin string,consent ImportMaskQualifier)(ImportPersonMaskQualificationInstallation,error){
 var zero ImportPersonMaskQualificationInstallation
 if err:=ctx.Err();err!=nil{return zero,err}
 if consent==nil{return zero,v2input.ErrInvalid}
 if len(raw)<2||len(raw)>1<<20||!validImportRefForSHA(pin)||rawSHA(raw)!=pin{return zero,v2input.ErrInvalid}
 var wire importPersonMaskDeploymentWire;if err:=decodeImportDocument(raw,&wire);err!=nil{return zero,err}
 if wire.Schema!="interchange-person-mask-qualification/v1"||!validImportRef(wire.Report)||wire.Locks.Operation!="mask.person"||wire.Locks.Version!="1"{return zero,v2input.ErrInvalid}
 if _,err:=wire.Locks.Expected();err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 return (ImportPersonMaskQualificationInstallation{Report:wire.Report,Locks:wire.Locks,Consent:consent}).owned(),nil
}

package interchange
import (
 "context"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
type importDenoiseDeploymentWire struct{
 Schema string `json:"schema_version"`
 Report v2input.Ref `json:"qualification_report"`
 Locks v2qualification.DeploymentLocks `json:"locks"`
}
func DecodeImportDenoiseQualification(ctx context.Context,raw []byte,pin string)(ImportDenoiseQualificationInstallation,error){
 var zero ImportDenoiseQualificationInstallation
 if err:=ctx.Err();err!=nil{return zero,err}
 if len(raw)<2||len(raw)>1<<20||!validImportRefForSHA(pin)||rawSHA(raw)!=pin{return zero,v2input.ErrInvalid}
 var wire importDenoiseDeploymentWire;if err:=decodeImportDocument(raw,&wire);err!=nil{return zero,err}
 if wire.Schema!="interchange-denoise-qualification/v1"||!validImportRef(wire.Report)||wire.Locks.Operation!="image.denoise"||wire.Locks.Version!="1"{return zero,v2input.ErrInvalid}
 if _,err:=wire.Locks.Expected();err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 return (ImportDenoiseQualificationInstallation{Report:wire.Report,Locks:wire.Locks}).owned(),nil
}

package interchange
import (
 "context"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
type importStatsDeploymentWire struct{
 Schema string `json:"schema_version"`
 Report v2input.Ref `json:"qualification_report"`
 Locks v2qualification.DeploymentLocks `json:"locks"`
}
func DecodeImportStatsQualification(ctx context.Context,raw []byte,pin string)(ImportStatsQualificationInstallation,error){
 var zero ImportStatsQualificationInstallation
 if err:=ctx.Err();err!=nil{return zero,err}
 if len(raw)<2||len(raw)>1<<20||!validImportRefForSHA(pin)||rawSHA(raw)!=pin{return zero,v2input.ErrInvalid}
 var wire importStatsDeploymentWire;if err:=decodeImportDocument(raw,&wire);err!=nil{return zero,err}
 if wire.Schema!="interchange-stats-qualification/v1"||!validImportRef(wire.Report)||wire.Locks.Operation!="color.stats"||wire.Locks.Version!="1"{return zero,v2input.ErrInvalid}
 if _,err:=wire.Locks.Expected();err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 return (ImportStatsQualificationInstallation{Report:wire.Report,Locks:wire.Locks}).owned(),nil
}

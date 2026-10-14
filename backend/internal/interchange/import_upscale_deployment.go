package interchange
import (
 "context"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
type importUpscalePolicyWire struct{Report v2input.Ref `json:"qualification_report"`;Locks v2qualification.DeploymentLocks `json:"locks"`}
type importUpscaleDeploymentWire struct{Schema string `json:"schema_version"`;Scales map[int]importUpscalePolicyWire `json:"scales"`}
func DecodeImportUpscaleQualifications(ctx context.Context,raw []byte,pin string)(ImportUpscaleQualifications,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if len(raw)<2||len(raw)>1<<20||!validImportRefForSHA(pin)||rawSHA(raw)!=pin{return nil,v2input.ErrInvalid}
 var wire importUpscaleDeploymentWire;if err:=decodeImportDocument(raw,&wire);err!=nil{return nil,err}
 if wire.Schema!="interchange-upscale-qualification/v1"||len(wire.Scales)<1||len(wire.Scales)>2{return nil,v2input.ErrInvalid}
 installed:=ImportUpscaleQualifications{}
 for scale,config:=range wire.Scales{
  if (scale!=2&&scale!=4)||!validImportRef(config.Report)||config.Locks.Operation!="image.upscale"||config.Locks.Version!="1"{return nil,v2input.ErrInvalid}
  if _,err:=config.Locks.Expected();err!=nil{return nil,err}
  installed[scale]=ImportUpscaleQualificationInstallation{Report:config.Report,Locks:config.Locks}
 }
 if err:=ctx.Err();err!=nil{return nil,err}
 return installed.owned()
}

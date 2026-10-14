package interchange

import (
 "context"
 "photoeditor/backend/internal/v2input"
)

// Explicit operation routing; refusal never falls back to another model policy.
type ExportSeriesMaskPolicyRouter struct{Person,Prompt ExportSeriesMaskPolicyAuthority}
func(s ExportSeriesMaskPolicyRouter)ResolveExportSeriesMaskPolicy(ctx context.Context,origin ExportSeriesMaskOrigin,source v2input.Ref)(ExportSeriesMaskPolicyEvidence,error){
 if err:=ctx.Err();err!=nil{return ExportSeriesMaskPolicyEvidence{},err}
 if origin.Version!="1"{return ExportSeriesMaskPolicyEvidence{},v2input.ErrInvalid}
 var policy ExportSeriesMaskPolicyAuthority
 switch origin.Operation{case "mask.person":policy=s.Person;case "mask.prompt":policy=s.Prompt;default:return ExportSeriesMaskPolicyEvidence{},v2input.ErrInvalid}
 if policy==nil{return ExportSeriesMaskPolicyEvidence{},v2input.ErrInvalid}
 return policy.ResolveExportSeriesMaskPolicy(ctx,origin,source)
}
var _ ExportSeriesMaskPolicyAuthority=ExportSeriesMaskPolicyRouter{}

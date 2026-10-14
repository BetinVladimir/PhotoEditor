package interchange
import (
 "context"
 "sort"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
// Each model policy must prove current runtime/model/consent admission. Manual
// raster contents remain independently subject to physical NPY qualification.
type ImportModelMaskPolicy struct{ModelLock string;Qualifier ImportMaskQualifier}
type ImportMaskPolicyInstallation struct{Manual bool;Models map[string]ImportModelMaskPolicy}
type importMaskSelectorPolicy struct{manual bool;models map[string]ImportModelMaskPolicy}
func(i ImportMaskPolicyInstallation)owned()(ImportMaskPolicyInstallation,error){
 var zero ImportMaskPolicyInstallation
 if (!i.Manual&&len(i.Models)==0)||len(i.Models)>1024{return zero,v2input.ErrInvalid}
 owned:=ImportMaskPolicyInstallation{Manual:i.Manual,Models:map[string]ImportModelMaskPolicy{}}
 for selector,policy:=range i.Models{
  if selector==""||selector=="manual"||len(selector)>120||!validImportRefForSHA(policy.ModelLock)||policy.Qualifier==nil{return zero,v2input.ErrInvalid}
  owned.Models[selector]=policy
 }
 return owned,nil
}
func(i ImportMaskPolicyInstallation)Build(ctx context.Context,masters ImportMasterReader,targets ImportBackgroundTargetAuthority)(ImportMaskQualifier,[]string,error){
 if err:=ctx.Err();err!=nil{return nil,nil,err}
 if masters==nil||targets==nil{return nil,nil,v2input.ErrInvalid}
 owned,err:=i.owned();if err!=nil{return nil,nil,err}
 locks:=[]string{};seen:=map[string]bool{}
 for _,policy:=range owned.Models{if !seen[policy.ModelLock]{seen[policy.ModelLock]=true;locks=append(locks,policy.ModelLock)}}
 sort.Strings(locks)
 return BoundImportMaskSemantics{Masters:masters,Targets:targets,Policy:importMaskSelectorPolicy{manual:owned.Manual,models:owned.Models}},locks,ctx.Err()
}
func(q importMaskSelectorPolicy)QualifyImportedMask(ctx context.Context,mask review.MaskRef,binding v2input.SessionVersionBinding)error{
 if err:=ctx.Err();err!=nil{return err}
 raw,err:=canonical.Marshal(mask);if err!=nil{return err};owned,err:=review.ParseMaskRef(raw);if err!=nil{return err}
 if owned.Selector=="manual"{
  if !q.manual||owned.ModelLockID!=nil{return &ImportRecipeUnsupported{Reason:"manual_mask_policy_not_installed"}}
  // Source/geometry/current editor authority belongs to the outer Bound
  // validator. No model, confirmation or approval is inherited here.
  return ctx.Err()
 }
 policy,ok:=q.models[owned.Selector]
 if !ok||policy.Qualifier==nil||owned.ModelLockID==nil||*owned.ModelLockID!=policy.ModelLock{return &ImportRecipeUnsupported{Reason:"mask_selector_model_not_installed"}}
 if err:=policy.Qualifier.QualifyImportedMask(ctx,owned,binding);err!=nil{return err};return ctx.Err()
}

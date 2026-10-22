package v2region

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type regionExecutionResolver struct{
 base RegionInputResolver
 catalog regionExecutionCatalog
 plan FrozenRegionPlan
}

func(r regionExecutionResolver) ResolveRegionInputs(ctx context.Context,who auth.Identity,tenant uuid.UUID,binding v2input.SessionVersionBinding,style v2input.Ref,delta v2draft.CorrectionVector)([]v2input.Entry,error){
 doc,err:=r.plan.decode();if err!=nil{return nil,err}
 if doc.Execution==nil||tenant!=doc.TenantID||binding!=doc.FrozenRecipe.Source||style!=doc.FrozenRecipe.BaseStyle||delta!=doc.FrozenRecipe.EffectiveDelta{return nil,ErrRegionInvalid}
 base,err:=r.base.ResolveRegionInputs(ctx,who,tenant,binding,style,delta);if err!=nil{return nil,err}
 base=append(base,v2input.Entry{Ref:doc.Recipe.Ref,Contract:"FrozenRecipe"})
 catalog:=r.catalog;catalog.Actor=who.UserID;catalog.Session=who.SessionID
 candidates,err:=catalog.candidates(ctx);if err!=nil{return nil,err}
 // Re-read every current canonical receipt, document and typed occurrence.
 // A candidate becoming ambiguous after preparation cannot reuse stale pins.
 if len(candidates)!=1{return nil,ErrRegionUnavailable}
 records,err:=catalog.records(ctx,candidates[0]);if err!=nil{return nil,err}
 sourceObject,_,err:=catalog.document(ctx,candidates[0].RunID,doc.Execution.Stages[0].TaskID,binding.Source);if err!=nil{return nil,err}
 sourceRaw,err:=readRegionDocument(ctx,catalog.Objects,sourceObject,binding.Source);if err!=nil{return nil,err}
 source,err:=review.ParseWorkingImage(sourceRaw);if err!=nil{return nil,err}
 actual,err:=compileRegionExecution(ctx,binding.Source,candidates[0].Target,style,source,records);if err!=nil{return nil,err}
 if len(candidates[0].CullingAuthority)>0&&string(candidates[0].CullingAuthority)!="null"{actual.CullingAuthority=candidates[0].CullingAuthority}
 expectedHash,e1:=canonical.Hash(doc.Execution);actualHash,e2:=canonical.Hash(actual)
 if e1!=nil||e2!=nil||expectedHash!=actualHash{return nil,ErrRegionInvalid}
 return catalog.executionInputs(ctx,candidates[0],actual,base)
}

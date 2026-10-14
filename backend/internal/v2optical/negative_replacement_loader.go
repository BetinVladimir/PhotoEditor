package v2optical

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

// LoadNegativeReplacementInputs loads the complete original-version base-route
// package from durable catalogues and object storage. No cancellation, staging
// or publication is initiated. Settlement and current source authority must be
// rechecked by Lock before any replacement mutation.
func LoadNegativeReplacementInputs(ctx context.Context,db NegativeTransactions,objects v2input.SessionAnalysisObjectReader,bucket string,who auth.Identity,tenant,project uuid.UUID,decision NegativeDeclinedDecision)(PreparedNegativeReplacementInputs,error){
 var zero PreparedNegativeReplacementInputs
 if err:=ctx.Err();err!=nil{return zero,err}
 if db==nil||objects==nil||len(bucket)<3||len(bucket)>63||tenant==uuid.Nil||project==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||decision.Review==uuid.Nil||decision.OriginTask==uuid.Nil||decision.OriginRun==uuid.Nil||decision.Route!="base"{return zero,v2input.ErrInvalid}
 if _,err:=domain.ParseDigest(decision.DecisionHash);err!=nil{return zero,v2input.ErrInvalid}
 plan,inventory,err:=ReadNegativeDependencyPackage(ctx,db,who,tenant,project,decision.OriginTask,decision.OriginRun,decision.Route,4096,65536);if err!=nil{return zero,err}
 selection,err:=LoadNegativeSelectedSource(ctx,db,objects,bucket,who,tenant,project,decision);if err!=nil{return zero,err}
 physical,err:=LoadNegativeSelectedArrays(ctx,db,objects,bucket,who,selection);if err!=nil{return zero,err}
 intents:=make([]VerifiedOpticalFrozenIntent,0,len(plan.AffectedTasks))
 for _,task:=range plan.AffectedTasks{
  if err:=ctx.Err();err!=nil{return zero,err}
  intent,err:=LoadOpticalFrozenIntent(ctx,db,objects,bucket,who,tenant,project,decision.Review,task,decision.DecisionHash);if err!=nil{return zero,err}
  intents=append(intents,intent)
 }
 // Object reads may span transactions. Refuse graph changes during this load;
 // the eventual mutation must still independently revalidate under locks.
 current,currentInventory,err:=ReadNegativeDependencyPackage(ctx,db,who,tenant,project,decision.OriginTask,decision.OriginRun,decision.Route,4096,65536);if err!=nil{return zero,err}
 packageSHA:=func(plan NegativeInvalidationPlan,inventory []NegativeDependencyTask)(string,error){
  return canonical.Hash(struct{Plan NegativeInvalidationPlan `json:"plan"`;Inventory []NegativeDependencyTask `json:"inventory"`}{plan,inventory})
 }
 before,err:=packageSHA(plan,inventory);if err!=nil{return zero,err}
 after,err:=packageSHA(current,currentInventory);if err!=nil{return zero,err}
 if before!=after{return zero,v2input.ErrExpired}
 return PrepareNegativeReplacementInputs(ctx,bucket,who,tenant,project,decision.Review,decision.DecisionHash,plan,inventory,physical,intents)
}

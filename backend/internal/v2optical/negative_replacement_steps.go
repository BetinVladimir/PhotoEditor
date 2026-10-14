package v2optical

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

// PreparedNegativeReplacementStep owns a ready node and its original intent.
// Readiness is advisory until admission verifies adopted parent output receipts.
// No operation or Params can be supplied by the caller in place of this intent.
type PreparedNegativeReplacementStep struct {
 node NegativeReplacementNode
 intent VerifiedOpticalFrozenIntent
}
func(s PreparedNegativeReplacementStep)Node()NegativeReplacementNode{
 node:=s.node;node.Parents=append([]NegativeReplacementParent(nil),node.Parents...);return node
}
func(s PreparedNegativeReplacementStep)Operation()string{return s.intent.locator.Operation}
func(s PreparedNegativeReplacementStep)Rebind(ctx context.Context,bindings []OpticalReplacementBinding,lines *v2input.Ref)(ReboundOpticalIntent,error){
 if s.node.OriginalTask==uuid.Nil||s.node.Task==uuid.Nil||s.node.Run==uuid.Nil||s.intent.locator.Task!=s.node.OriginalTask||s.intent.locator.Run!=s.node.OriginalRun{return ReboundOpticalIntent{},v2input.ErrInvalid}
 v:=s.intent;l:=v.locator
 return v.Rebind(ctx,v.who,l.Tenant,l.Project,l.Review,l.Task,l.Hash,bindings,lines)
}

func(p PreparedNegativeReplacementInputs)checked(ctx context.Context)(PreparedNegativeReplacementInputs,NegativeInvalidationPlan,[]NegativeDependencyTask,error){
 var plan NegativeInvalidationPlan;var inventory []NegativeDependencyTask
 if err:=ctx.Err();err!=nil{return PreparedNegativeReplacementInputs{},plan,nil,err}
 if p.fingerprint==""||json.Unmarshal([]byte(p.planJCS),&plan)!=nil||json.Unmarshal([]byte(p.inventoryJCS),&inventory)!=nil{return PreparedNegativeReplacementInputs{},plan,nil,v2input.ErrInvalid}
 checked,err:=PrepareNegativeReplacementInputs(ctx,p.bucket,p.who,p.tenant,p.project,p.review,p.hash,plan,inventory,p.physical,p.intents)
 if err!=nil{return PreparedNegativeReplacementInputs{},plan,nil,err}
 if checked.fingerprint!=p.fingerprint{return PreparedNegativeReplacementInputs{},plan,nil,v2input.ErrInvalid}
 return checked,plan,inventory,nil
}

// ReadySteps validates the complete privately owned input packet before matching
// ready replacement identities to exact frozen operation intents. Completion IDs
// remain hints, never queue receipts or proof that the parents were adopted.
func(p PreparedNegativeReplacementInputs)ReadySteps(ctx context.Context,completed []uuid.UUID)([]PreparedNegativeReplacementStep,error){
 checked,plan,inventory,err:=p.checked(ctx);if err!=nil{return nil,err}
 topology,err:=checked.Topology();if err!=nil{return nil,err}
 nodes,err:=CompileNegativeReplacementFrontier(ctx,topology,plan,inventory,completed);if err!=nil{return nil,err}
 intents:=make(map[uuid.UUID]VerifiedOpticalFrozenIntent,len(checked.intents));for _,v:=range checked.intents{intents[v.locator.Task]=v}
 out:=make([]PreparedNegativeReplacementStep,0,len(nodes))
 for _,node:=range nodes{
  intent,ok:=intents[node.OriginalTask];if !ok||intent.locator.Run!=node.OriginalRun{return nil,v2input.ErrInvalid}
  out=append(out,PreparedNegativeReplacementStep{node:node,intent:intent})
 }
 return out,ctx.Err()
}

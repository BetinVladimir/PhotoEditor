package v2optical

import (
 "bytes"
 "context"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// LockAffectedSources revalidates the complete input package, exact topology
// node/frozen intent and persisted occurrence map, then retains SQL authority.
// No IO/commit/publication. Caller must roll back every error and keep this
// transaction through all qualification and replacement admission checks.
func(p PreparedNegativeReplacementInputs)LockAffectedSources(ctx context.Context,tx pgx.Tx,mapping VerifiedNegativeAffectedSources)error{
 if err:=ctx.Err();err!=nil{return err};if tx==nil{return v2input.ErrInvalid}
 checked,_,_,err:=p.checked(ctx);if err!=nil{return err}
 topology,err:=checked.Topology();if err!=nil{return err}
 nodeRaw,err:=canonical.Marshal(mapping.step.node);if err!=nil{return err};found:=false
 for _,node:=range topology.Nodes{raw,err:=canonical.Marshal(node);if err!=nil{return err};if bytes.Equal(raw,nodeRaw){found=true}}
 if !found{return v2input.ErrInvalid}
 frozenFound:=false
 for _,intent:=range checked.intents{if intent.locator==mapping.step.intent.locator&&intent.raw==mapping.step.intent.raw&&intent.who.UserID==mapping.step.intent.who.UserID&&intent.who.SessionID==mapping.step.intent.who.SessionID{frozenFound=true}}
 if !frozenFound{return v2input.ErrInvalid}
 matched,err:=matchNegativeAffectedSources(ctx,mapping.step.intent.locator.Operation,[]byte(mapping.step.intent.raw),mapping.step.node.Parents,[]byte(mapping.raw));if err!=nil{return err}
 actual,err:=canonical.Marshal(matched);if err!=nil{return err};expected,err:=canonical.Marshal(mapping.sources)
 if err!=nil||!bytes.Equal(actual,expected){return v2input.ErrInvalid}
 var role string;if err=tx.QueryRow(ctx,"SELECT current_user").Scan(&role);err!=nil{return err};if role!="photo_scheduler"{return v2input.ErrInvalid}
 if _,err=tx.Exec(ctx,"SELECT set_config('app.tenant_id',$1,true)",checked.tenant.String());err!=nil{return err}
 var allowed bool
 if err=tx.QueryRow(ctx,"SELECT photo.lock_v29_negative_affected_sources($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9::jsonb)",checked.tenant,checked.project,checked.who.UserID,checked.who.SessionID,checked.review,checked.hash,checked.planJCS,mapping.step.node.OriginalTask,mapping.raw).Scan(&allowed);err!=nil{return err}
 if !allowed{return v2input.ErrExpired};return ctx.Err()
}

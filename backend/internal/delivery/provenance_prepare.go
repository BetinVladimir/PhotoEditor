package delivery

import (
 "context"
 "time"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2input"
)

// PrepareSigningDependencies composes the frozen Delivery slot inventory with
// the existing V31 live export/policy checks. It is read-only: no signer task
// is admitted and no Delivery slot becomes ready by preparing an intent.
func PrepareSigningDependencies(ctx context.Context, who auth.Identity, tenant, project, parent uuid.UUID, frozen FrozenInput,
 slots []Slot, source provenance.SigningExportSource, registry *provenance.SigningPolicyRegistry, now time.Time,
) ([]provenance.FrozenSigningIntent,error) {
 if err:=ctx.Err();err!=nil{return nil,err}
 if who.UserID==uuid.Nil||who.SessionID==uuid.Nil||tenant==uuid.Nil||project==uuid.Nil||source==nil||registry==nil||now.IsZero(){return nil,v2input.ErrInvalid}
 choice,err:=DeliverySigningChoice(frozen);if err!=nil{return nil,err}
 deps,err:=CompileSigningDependencies(parent,choice.PolicyID,slots);if err!=nil{return nil,err}
 tenantRegistry,err:=registry.ForTenant(tenant);if err!=nil{return nil,err}
 out:=make([]provenance.FrozenSigningIntent,0,len(deps))
 for _,dependency:=range deps {
  if err:=ctx.Err();err!=nil{return nil,err}
  intent,err:=provenance.PrepareSigningIntent(ctx,who,tenant,dependency.ExportID,dependency.PolicyID,
   dependency.IdempotencyKey,source,tenantRegistry,now)
  if err!=nil{return nil,err}
  params,err:=provenance.DecodeSignParams(intent.ParamsBytes())
  if err!=nil||intent.ProjectID()!=project||params.Export!=dependency.Export||
   params.PolicyID!=choice.PolicyID||params.Policy.SHA256!=choice.PolicySHA{
   return nil,v2input.ErrInvalid
  }
  out=append(out,intent)
 }
 return out,nil
}

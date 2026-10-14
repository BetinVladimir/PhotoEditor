package delivery

import (
 "context"
 "fmt"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2input"
)

type SignedDependencyRead func(context.Context)(SignedDependencyReceipt,bool,error)

// VerifySigningDependencyCurrent holds no SQL transaction across object IO.
// It repeats durable receipt and live operator-policy checks afterward. A
// successful result is evidence for a later atomic assembly admission, not a
// permission to publish Delivery without that admission's own current checks.
func VerifySigningDependencyCurrent(ctx context.Context,who auth.Identity,tenant,project,parent uuid.UUID,
 dependency SigningDependency,intent provenance.FrozenSigningIntent,policySHA string,
 registry *provenance.SigningPolicyRegistry,clock func()time.Time,
 objects SignedDependencyObjectReader,read SignedDependencyRead)(SignedDependencyReceipt,error){
 var zero SignedDependencyReceipt
 if err:=ctx.Err();err!=nil{return zero,err}
 if who.UserID==uuid.Nil||who.SessionID==uuid.Nil||tenant==uuid.Nil||project==uuid.Nil||parent==uuid.Nil||
  registry==nil||clock==nil||objects==nil||read==nil||!shaPattern.MatchString(policySHA)||
  intent.ProjectID()!=project||dependency.PolicyID==uuid.Nil||dependency.ExportID==uuid.Nil||
  dependency.PresetID==uuid.Nil||dependency.Ordinal<0||dependency.Ordinal>4999||
  dependency.IdempotencyKey!=uuid.NewSHA1(parent,[]byte(fmt.Sprintf("%d:%s",dependency.Ordinal,dependency.PresetID))).String(){return zero,v2input.ErrInvalid}
 expectedWorkflow:=uuid.NewSHA1(tenant,[]byte("provenance:"+who.UserID.String()+":"+dependency.IdempotencyKey))
 if intent.WorkflowID()!=expectedWorkflow||intent.TaskID()!=uuid.NewSHA1(expectedWorkflow,[]byte("provenance.sign:0")){
  return zero,v2input.ErrInvalid
 }
 params,err:=provenance.DecodeSignParams(intent.ParamsBytes())
 if err!=nil||params.Export!=dependency.Export||params.PolicyID!=dependency.PolicyID||params.Policy.SHA256!=policySHA{
  return zero,v2input.ErrInvalid
 }
 tenantRegistry,err:=registry.ForTenant(tenant);if err!=nil{return zero,err}
 first,found,err:=read(ctx);if err!=nil{return zero,err};if !found||!validSignedDependencyReceipt(dependency,first)||first.TaskID!=intent.TaskID(){return zero,v2input.ErrInvalid}
 now:=clock();if now.IsZero(){return zero,v2input.ErrInvalid}
 if _,err:=tenantRegistry.Resolve(ctx,params,first.SignedObject.ContentType,now);err!=nil{return zero,err}
 if err:=VerifySignedDependencyObjects(ctx,tenant,first,objects);err!=nil{return zero,err}
 current,found,err:=read(ctx);if err!=nil{return zero,err}
 if !found||current!=first{return zero,v2input.ErrInvalid}
 now=clock();if now.IsZero(){return zero,v2input.ErrInvalid}
 if _,err:=tenantRegistry.Resolve(ctx,params,current.SignedObject.ContentType,now);err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 return current,nil
}

func LoadVerifiedSigningDependency(ctx context.Context,db *pgxpool.Pool,who auth.Identity,tenant,project,parent uuid.UUID,
 dependency SigningDependency,intent provenance.FrozenSigningIntent,policySHA,bucket string,
 registry *provenance.SigningPolicyRegistry,clock func()time.Time,objects SignedDependencyObjectReader)(SignedDependencyReceipt,error){
 if db==nil||bucket==""{return SignedDependencyReceipt{},v2input.ErrInvalid}
 read:=func(call context.Context)(SignedDependencyReceipt,bool,error){
  return ReadSignedDependencyReceipt(call,db,who,tenant,project,dependency,policySHA,bucket)
 }
 return VerifySigningDependencyCurrent(ctx,who,tenant,project,parent,dependency,intent,policySHA,registry,clock,objects,read)
}

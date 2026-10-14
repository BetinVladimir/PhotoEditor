package delivery

import (
 "context"
 "time"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/provenance"
 "photoeditor/backend/internal/v2input"
)

type SignedClosureCoordinator struct {
 Store ExpansionLeaseStore
 Receipts *pgxpool.Pool
 Registry *provenance.SigningPolicyRegistry
 Objects SignedDependencyObjectReader
 Bucket string
 Clock func()time.Time
}

type signedClosureVerify func(context.Context,SigningDependency,provenance.FrozenSigningIntent)(SignedDependencyReceipt,error)

// collectSignedClosureEvidence enforces the exact frozen slot set and intent
// order before any retained state is changed. The production verifier performs
// tenant-scoped SQL/policy reads and exact-version physical object streaming.
func collectSignedClosureEvidence(ctx context.Context,parent,policy uuid.UUID,slots []Slot,
 intents []provenance.FrozenSigningIntent,verify signedClosureVerify)([]SigningDependency,[]SignedDependencyReceipt,error){
 if err:=ctx.Err();err!=nil{return nil,nil,err}
 if verify==nil{return nil,nil,v2input.ErrInvalid}
 dependencies,err:=CompileSigningDependencies(parent,policy,slots)
 if err!=nil||len(intents)!=len(dependencies){return nil,nil,v2input.ErrInvalid}
 receipts:=make([]SignedDependencyReceipt,0,len(dependencies))
 for i,dependency:=range dependencies{
  if err:=ctx.Err();err!=nil{return nil,nil,err}
  receipt,err:=verify(ctx,dependency,intents[i]);if err!=nil{return nil,nil,err}
  if !validSignedDependencyReceipt(dependency,receipt)||receipt.TaskID!=intents[i].TaskID(){return nil,nil,v2input.ErrInvalid}
  receipts=append(receipts,receipt)
 }
 return dependencies,receipts,nil
}

func(c SignedClosureCoordinator)current(ctx context.Context,who auth.Identity,lease ExpansionLease,
 project uuid.UUID,frozen FrozenInput,slots []Slot,intents []provenance.FrozenSigningIntent)([]SigningDependency,[]SignedDependencyReceipt,error){
 choice,choiceErr:=DeliverySigningChoice(frozen);if choiceErr!=nil{return nil,nil,choiceErr}
 if !lease.valid()||lease.Cursor!=lease.SlotCount||len(slots)!=lease.SlotCount||
  who.UserID==uuid.Nil||who.SessionID==uuid.Nil||project==uuid.Nil||
  c.Store.DB==nil||c.Receipts==nil||c.Registry==nil||c.Objects==nil||
  c.Bucket==""||c.Clock==nil{return nil,nil,v2input.ErrInvalid}
 observed,err:=ObserveDeliverySigningSet(ctx,c.Store.DB,lease,who.UserID,choice.PolicyID,slots,
  func(call context.Context)error{return c.Store.Renew(call,lease)})
 if err!=nil{return nil,nil,err}
 if observed.State=="failed"{return nil,nil,domain.NewError("DELIVERY_SIGNING_FAILED",409)}
 if observed.State!="adopted"{return nil,nil,domain.NewError("DELIVERY_SIGNING_PENDING",409)}
 verify:=func(call context.Context,dependency SigningDependency,intent provenance.FrozenSigningIntent)(SignedDependencyReceipt,error){
  return LoadVerifiedSigningDependency(call,c.Receipts,who,lease.Tenant,project,lease.Run,
   dependency,intent,choice.PolicySHA,c.Bucket,c.Registry,c.Clock,c.Objects)
 }
 return collectSignedClosureEvidence(ctx,lease.Run,choice.PolicyID,slots,intents,verify)
}

// VerifyAndRetainSigningClosure is the production boundary from physically
// verified V31 results to lease-fenced V30 durable dependency evidence.
func(c SignedClosureCoordinator)VerifyAndRetainSigningClosure(ctx context.Context,who auth.Identity,
 lease ExpansionLease,project uuid.UUID,frozen FrozenInput,slots []Slot,
 intents []provenance.FrozenSigningIntent)(SignedAssemblyClosure,error){
 choice,err:=DeliverySigningChoice(frozen);if err!=nil{return SignedAssemblyClosure{},err}
 dependencies,receipts,err:=c.current(ctx,who,lease,project,frozen,slots,intents)
 if err!=nil{return SignedAssemblyClosure{},err}
 return c.Store.RetainVerifiedSigningClosure(ctx,lease,who.UserID,choice.PolicyID,choice.PolicySHA,slots,dependencies,receipts)
}

// VerifyAndReadSigningClosure repeats all physical and policy checks after a
// restart, then requires the exact saved closure for the same frozen matrix.
func(c SignedClosureCoordinator)VerifyAndReadSigningClosure(ctx context.Context,who auth.Identity,
 lease ExpansionLease,project uuid.UUID,frozen FrozenInput,slots []Slot,
 intents []provenance.FrozenSigningIntent)(SignedAssemblyClosure,error){
 choice,err:=DeliverySigningChoice(frozen);if err!=nil{return SignedAssemblyClosure{},err}
 dependencies,receipts,err:=c.current(ctx,who,lease,project,frozen,slots,intents)
 if err!=nil{return SignedAssemblyClosure{},err}
 return c.Store.ReadRetainedSigningClosure(ctx,lease,who.UserID,choice.PolicyID,choice.PolicySHA,slots,dependencies,receipts)
}

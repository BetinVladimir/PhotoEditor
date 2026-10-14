package delivery

import (
 "context"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

type SignedAssemblyDocuments struct {
 Assembly AssemblyInput
 AssemblyJCS []byte
 AssemblySHA string
 Closure SignedAssemblyClosure
 ClosureJCS []byte
 ClosureSHA string
}

// BuildSignedAssemblyDocuments is the private required-mode assembly compiler.
// The caller must have verified V31 receipts, operator policy and physical
// signed bytes before invoking it. Shards may be published only after a full
// closure preflight, and final SQL activation must revalidate that closure.
func BuildSignedAssemblyDocuments(ctx context.Context,frozen FrozenInput,snapshotRaw []byte,
 slots []Slot,tenant,actor,parent uuid.UUID,dependencies []SigningDependency,
 receipts []SignedDependencyReceipt,publish PublishDocument)(SignedAssemblyDocuments,error){
 if err:=ctx.Err();err!=nil{return SignedAssemblyDocuments{},err}
 choice,err:=DeliverySigningChoice(frozen)
 if err!=nil||tenant==uuid.Nil||actor==uuid.Nil||parent==uuid.Nil||publish==nil{return SignedAssemblyDocuments{},v2input.ErrInvalid}
 closure,closureRaw,closureSHA,err:=CompileSignedAssemblyClosureForSlots(tenant,actor,parent,
  choice.PolicyID,choice.PolicySHA,slots,dependencies,receipts)
 if err!=nil{return SignedAssemblyDocuments{},err}
 assembly,assemblyRaw,assemblySHA,err:=buildAssemblyInput(ctx,frozen,snapshotRaw,slots,publish,true)
 if err!=nil{return SignedAssemblyDocuments{},err}
 return SignedAssemblyDocuments{assembly,assemblyRaw,assemblySHA,closure,closureRaw,closureSHA},nil
}

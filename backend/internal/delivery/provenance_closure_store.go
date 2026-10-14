package delivery

import (
 "context"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

// RetainVerifiedSigningClosure persists a complete frozen-slot witness under
// the current expansion lease. SQL repeats current export/adoption authority;
// the result is still only evidence for a later assembly admission.
func (s ExpansionLeaseStore) RetainVerifiedSigningClosure(ctx context.Context,lease ExpansionLease,
 actor,policy uuid.UUID,policySHA string,slots []Slot,dependencies []SigningDependency,
 receipts []SignedDependencyReceipt)(SignedAssemblyClosure,error){
 if err:=ctx.Err();err!=nil{return SignedAssemblyClosure{},err}
 if s.DB==nil||!lease.valid()||lease.Cursor!=lease.SlotCount||actor==uuid.Nil||policy==uuid.Nil||
  len(slots)!=lease.SlotCount{return SignedAssemblyClosure{},v2input.ErrInvalid}
 closure,raw,sha,err:=CompileSignedAssemblyClosureForSlots(lease.Tenant,actor,lease.Run,policy,policySHA,
  slots,dependencies,receipts)
 if err!=nil{return SignedAssemblyClosure{},err}
 var accepted bool
 if err:=s.DB.QueryRow(ctx,`SELECT photo.retain_v30_delivery_signing_closure($1,$2,$3,$4)`,
  lease.Expansion,lease.Token,sha,string(raw)).Scan(&accepted);err!=nil{return SignedAssemblyClosure{},err}
 if !accepted{return SignedAssemblyClosure{},domain.NewError("DELIVERY_SIGNING_CLOSURE_STALE",409)}
 return closure,nil
}

// ReadRetainedSigningClosure recovers an exact persisted closure and rejects
// any slot, dependency, receipt or policy change since retention. This is
// restart evidence, not permission to activate or publish the assembly.
func (s ExpansionLeaseStore) ReadRetainedSigningClosure(ctx context.Context,lease ExpansionLease,
 actor,policy uuid.UUID,policySHA string,slots []Slot,dependencies []SigningDependency,
 receipts []SignedDependencyReceipt)(SignedAssemblyClosure,error){
 if err:=ctx.Err();err!=nil{return SignedAssemblyClosure{},err}
 if s.DB==nil||!lease.valid()||lease.Cursor!=lease.SlotCount||actor==uuid.Nil||policy==uuid.Nil||
  len(slots)!=lease.SlotCount{return SignedAssemblyClosure{},v2input.ErrInvalid}
 var raw,sha string
 err:=s.DB.QueryRow(ctx,`SELECT closure_jcs,closure_sha FROM photo.read_v30_delivery_signing_closure($1,$2)`,
  lease.Expansion,lease.Token).Scan(&raw,&sha)
 if err==pgx.ErrNoRows{return SignedAssemblyClosure{},domain.NewError("DELIVERY_SIGNING_CLOSURE_STALE",409)}
 if err!=nil{return SignedAssemblyClosure{},err}
 if !shaPattern.MatchString(sha)||digest([]byte(raw))!=sha{return SignedAssemblyClosure{},v2input.ErrInvalid}
 return MatchSignedAssemblyClosureForSlots([]byte(raw),lease.Tenant,actor,lease.Run,policy,policySHA,
  slots,dependencies,receipts)
}

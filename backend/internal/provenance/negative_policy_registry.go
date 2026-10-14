package provenance

import (
 "context"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2optical"
 "photoeditor/backend/internal/v2qualification"
)

type InstalledSigningNegativePolicy struct {
 PolicyID uuid.UUID
 PolicySHA string
 Locks v2qualification.DeploymentLocks
}
type signingNegativePolicyKey struct {ID uuid.UUID; SHA string}
type SigningNegativePolicyRegistry struct {policies map[signingNegativePolicyKey]v2qualification.DeploymentLocks}

func ownSigningNegativeLocks(locks v2qualification.DeploymentLocks)v2qualification.DeploymentLocks{
 locks.RequiredChecks=append([]string(nil),locks.RequiredChecks...)
 locks.CalibrationSHAs=append([]string(nil),locks.CalibrationSHAs...)
 locks.TestFixtureSHAs=append([]string(nil),locks.TestFixtureSHAs...)
 locks.EvidenceSHAs=append([]string(nil),locks.EvidenceSHAs...)
 return locks
}

// Entries originate only in authenticated process provisioning. Old policy
// revisions may coexist for historical provenance; no revision is inferred from
// report bytes and there is no fallback to the latest/current runtime locks.
func NewSigningNegativePolicyRegistry(entries []InstalledSigningNegativePolicy)(*SigningNegativePolicyRegistry,error){
 if len(entries)<1||len(entries)>256{return nil,ErrInvalid}
 registry:=&SigningNegativePolicyRegistry{policies:map[signingNegativePolicyKey]v2qualification.DeploymentLocks{}}
 total:=0
 for _,entry:=range entries{
  locks:=ownSigningNegativeLocks(entry.Locks)
  raw,err:=canonical.Marshal(locks);if err!=nil||len(raw)>1<<20{return nil,ErrInvalid}
  total+=len(raw);if total>8<<20{return nil,ErrInvalid}
  sha,err:=v2optical.NegativePolicySHA(entry.PolicyID,locks)
  if err!=nil||!digest(entry.PolicySHA)||sha!=entry.PolicySHA{return nil,ErrInvalid}
  key:=signingNegativePolicyKey{entry.PolicyID,sha}
  if _,exists:=registry.policies[key];exists{return nil,ErrInvalid}
  registry.policies[key]=locks
 }
 return registry,nil
}

func(r *SigningNegativePolicyRegistry)Resolve(ctx context.Context,pin v2optical.NegativeQualificationPin)(v2qualification.DeploymentLocks,error){
 var zero v2qualification.DeploymentLocks
 if err:=ctx.Err();err!=nil{return zero,err}
 if r==nil{return zero,ErrInvalid};if _,err:=pin.Compile();err!=nil{return zero,err}
 locks,exists:=r.policies[signingNegativePolicyKey{pin.PolicyID,pin.PolicySHA}]
 if !exists{return zero,ErrInvalid}
 locks=ownSigningNegativeLocks(locks)
 expected,err:=locks.Expected()
 if err!=nil||expected.ModelLockSHA!=pin.ModelLockSHA||expected.RuntimeLockSHA!=pin.RuntimeLockSHA||expected.DeviceProfileSHA!=pin.DeviceProfileSHA||expected.DatasetSHA!=pin.DatasetSHA||expected.IssuerID!=pin.IssuerID||expected.Backend!=pin.Backend{return zero,ErrInvalid}
 if _,exists:=expected.CalibrationSHAs[pin.Calibration.SHA256];!exists{return zero,ErrInvalid}
 return locks,nil
}

type BoundSigningNegativeQualificationContent struct{
 Historical SigningNegativeQualificationReader
 Documents SigningTaskDocumentReader
 Policies *SigningNegativePolicyRegistry
}

func(s BoundSigningNegativeQualificationContent)Read(ctx context.Context,who auth.Identity,ready ReadySigningExport,task CanonicalSigningTask)(v2optical.VerifiedNegativeQualification,error){
 var zero v2optical.VerifiedNegativeQualification
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.Historical==nil||s.Documents==nil||s.Policies==nil||task.Operation!="optical.negative"||task.Version!="1"||normalizeCanonicalSigningTask(&task)!=nil{return zero,ErrInvalid}
 pin,err:=s.Historical.ReadSigningNegativeQualification(ctx,who,ready,task);if err!=nil{return zero,err}
 locks,err:=s.Policies.Resolve(ctx,pin);if err!=nil{return zero,err}
 result,err:=ReadSigningNegativeQualificationContent(ctx,s.Historical,s.Documents,who,ready,task,locks);if err!=nil{return zero,err}
 verified,err:=result.Pin();if err!=nil||verified!=pin{return zero,ErrInvalid}
 current,err:=s.Historical.ReadSigningNegativeQualification(ctx,who,ready,task);if err!=nil{return zero,err}
 if current!=pin{return zero,ErrInvalid}
 return result,ctx.Err()
}

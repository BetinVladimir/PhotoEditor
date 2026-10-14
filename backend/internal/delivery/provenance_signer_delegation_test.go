package delivery

import (
 "errors"
 "fmt"
 "testing"
 "time"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

func TestSignerDelegationScopeBindsExactParentSlot(t *testing.T){
 lease:=ExpansionLease{Expansion:uuid.New(),Token:uuid.New(),Until:time.Now().Add(time.Minute),
  Tenant:uuid.New(),Run:uuid.New(),Workflow:uuid.New(),Package:uuid.New(),
  ParentTask:uuid.New(),Cursor:1,SlotCount:1}
 export:=uuid.New();ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:signingObjectHash([]byte("export"))}
 slot:=Slot{Ordinal:0,PresetID:uuid.New(),State:"ready",ExportID:&export,Export:&ref}
 actor:=uuid.New();key:=uuid.NewSHA1(lease.Run,[]byte(fmt.Sprintf("%d:%s",slot.Ordinal,slot.PresetID))).String()
 workflow:=uuid.NewSHA1(lease.Tenant,[]byte("provenance:"+actor.String()+":"+key))
 scope:=SignerDelegationScope{TenantID:lease.Tenant,ActorID:actor,ProjectID:uuid.New(),
  ParentRunID:lease.Run,PackageID:lease.Package,Ordinal:slot.Ordinal,PresetID:slot.PresetID,
  ExportID:export,ExportArtifactID:ref.ArtifactID,ExportSHA:ref.SHA256,
  PolicyID:uuid.New(),PolicySHA:signingObjectHash([]byte("policy")),
  IdempotencyKey:key,WorkflowID:workflow,
  TaskID:uuid.NewSHA1(workflow,[]byte("provenance.sign:0"))}
 if err:=ValidateSignerDelegationScope(lease,slot,scope);err!=nil{t.Fatal(err)}
 foreign:=scope;foreign.ExportID=uuid.New()
 if err:=ValidateSignerDelegationScope(lease,slot,foreign);!errors.Is(err,v2input.ErrInvalid){
  t.Fatal("foreign export delegated",err)
 }
 foreign=scope;foreign.IdempotencyKey=uuid.NewString()
 if err:=ValidateSignerDelegationScope(lease,slot,foreign);!errors.Is(err,v2input.ErrInvalid){
  t.Fatal("foreign signer key delegated",err)
 }
}

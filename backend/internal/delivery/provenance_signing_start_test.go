package delivery

import (
 "context"
 "errors"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

type deliverySignerStub struct{calls int;failAt int;wrong bool;keys []string;policies []uuid.UUID}
func(s *deliverySignerStub)SignExport(_ context.Context,who auth.Identity,tenant,export,policy uuid.UUID,key string)(v2input.JobTicket,error){
 s.calls++;s.keys=append(s.keys,key);s.policies=append(s.policies,policy)
 if s.calls==s.failAt{return v2input.JobTicket{},errors.New("signer unavailable")}
 workflow:=uuid.NewSHA1(tenant,[]byte("provenance:"+who.UserID.String()+":"+key))
 if s.wrong{workflow=uuid.New()}
 return v2input.JobTicket{WorkflowID:workflow,RunID:uuid.NewSHA1(workflow,[]byte("run")),State:"queued"},nil
}

func TestStartDeliverySigningDependenciesUsesExactReplayableChildren(t *testing.T){
 sha:=strings.Repeat("a",64);who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 tenant,parent,policy:=uuid.New(),uuid.New(),uuid.New()
 frozen:=FrozenInput{SchemaVersion:1,Policy:Policy{Provenance:"required",SigningPolicyID:policy,SigningPolicySHA:sha}}
 binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},CanvasHash:sha}
 ready:=func(ordinal int)Slot{exportID:=uuid.New();ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}
  return Slot{Ordinal:ordinal,Binding:binding,PresetID:uuid.New(),PresetSHA:sha,Required:true,State:"ready",ExportID:&exportID,Export:&ref}}
 first,second:=ready(0),ready(1);reason:="OPTIONAL_FAILED"
 omitted:=Slot{Ordinal:2,Binding:binding,PresetID:uuid.New(),PresetSHA:sha,State:"omitted",Reason:&reason}
 slots:=[]Slot{second,omitted,first}
 off:=frozen;off.Policy.Provenance="off"
 blocked:=&deliverySignerStub{}
 if _,err:=StartDeliverySigningDependencies(context.Background(),who,tenant,parent,off,slots,blocked);err==nil||blocked.calls!=0{t.Fatal("off mode reached signer")}
 signer:=&deliverySignerStub{}
 children,err:=StartDeliverySigningDependencies(context.Background(),who,tenant,parent,frozen,slots,signer)
 if err!=nil||len(children)!=2||signer.calls!=2||children[0].Dependency.Ordinal!=0||children[1].Dependency.Ordinal!=1{t.Fatal("signer set",err)}
 replay:=&deliverySignerStub{}
 again,err:=StartDeliverySigningDependencies(context.Background(),who,tenant,parent,frozen,slots,replay)
 if err!=nil||len(again)!=2||again[0].Ticket!=children[0].Ticket||again[1].Ticket!=children[1].Ticket||replay.keys[0]!=signer.keys[0]{t.Fatal("signing replay",err)}
 partial:=&deliverySignerStub{failAt:2}
 if _,err:=StartDeliverySigningDependencies(context.Background(),who,tenant,parent,frozen,slots,partial);err==nil||partial.calls!=2{t.Fatal("partial failure hidden")}
 bad:=&deliverySignerStub{wrong:true}
 if _,err:=StartDeliverySigningDependencies(context.Background(),who,tenant,parent,frozen,slots,bad);err==nil||bad.calls!=1{t.Fatal("foreign ticket accepted")}
 first.State="pending";bad=&deliverySignerStub{}
 if _,err:=StartDeliverySigningDependencies(context.Background(),who,tenant,parent,frozen,[]Slot{first,second},bad);err==nil||bad.calls!=0{t.Fatal("unready matrix reached signer")}
 frozen.Policy.SigningPolicyID=uuid.New();bad=&deliverySignerStub{}
 if _,err:=StartDeliverySigningDependencies(context.Background(),who,tenant,parent,frozen,slots,bad);err!=nil||bad.calls!=2||bad.policies[0]!=frozen.Policy.SigningPolicyID{t.Fatal("frozen policy choice not used",err)}
}

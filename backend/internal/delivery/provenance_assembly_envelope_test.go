package delivery

import (
 "bytes"
 "fmt"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

func TestSignedAssemblyEnvelopeBindsRequiredMatrixAndClosure(t *testing.T){
 sha:=strings.Repeat("a",64)
 tenant,actor,parent,policy,preset:=uuid.New(),uuid.New(),uuid.New(),uuid.New(),uuid.New()
 exportID:=uuid.New();export:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}
 key:=uuid.NewSHA1(parent,[]byte(fmt.Sprintf("%d:%s",0,preset))).String()
 dependency:=SigningDependency{Ordinal:0,PresetID:preset,ExportID:exportID,Export:export,PolicyID:policy,IdempotencyKey:key}
 workflow:=uuid.NewSHA1(tenant,[]byte("provenance:"+actor.String()+":"+key))
 signed:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha};manifest:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}
 receipt:=SignedDependencyReceipt{Signed:signed,Manifest:manifest,ValidationSHA:sha,
  TaskID:uuid.NewSHA1(workflow,[]byte("provenance.sign:0")),ResultID:uuid.New(),Bucket:"artifacts",
  SignedObject:objectstore.Object{Key:"tenants/test/signed",VersionID:"v1",SHA256:sha,Bytes:42,ContentType:"image/jpeg"},
  ManifestObject:objectstore.Object{Key:"tenants/test/manifest",VersionID:"v2",SHA256:sha,Bytes:13,ContentType:"application/c2pa"}}
 exportCopy:=export
 binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},CanvasHash:sha}
 slot:=Slot{Ordinal:0,Binding:binding,PresetID:preset,PresetSHA:sha,Required:true,State:"ready",ExportID:&exportID,Export:&exportCopy}
 _,closureRaw,closureSHA,err:=CompileSignedAssemblyClosureForSlots(tenant,actor,parent,policy,sha,[]Slot{slot},[]SigningDependency{dependency},[]SignedDependencyReceipt{receipt})
 if err!=nil{t.Fatal(err)}
 assembly:=AssemblyInput{SchemaVersion:1,SnapshotDigest:sha,ItemCount:1,SlotCount:1,
  Policy:Policy{RequiredPresets:[]uuid.UUID{preset},OptionalPresets:[]uuid.UUID{},MetadataPolicy:"strip",Provenance:"required",SigningPolicyID:policy,SigningPolicySHA:sha},
  Presets:[]FrozenPreset{{PresetID:preset,PresetSHA:sha,Config:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},Required:true}},
  Shards:[]v2input.Ref{{ArtifactID:uuid.New(),SHA256:sha}}}
 shard,err:=canonical.Marshal(SlotShard{1,sha,[]Slot{slot}});if err!=nil{t.Fatal(err)}
 assembly.Shards[0].SHA256=digest(shard)
 shards:=[][]byte{shard}
 assemblyRaw,err:=canonical.Marshal(assembly);if err!=nil{t.Fatal(err)}
 assemblyRef:=v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(assemblyRaw)}
 closureRef:=v2input.Ref{ArtifactID:uuid.New(),SHA256:closureSHA}
 envelope,raw,hash,err:=CompileSignedAssemblyEnvelope(assemblyRef,assemblyRaw,shards,closureRef,closureRaw,tenant,actor,parent,[]Slot{slot},[]SigningDependency{dependency},[]SignedDependencyReceipt{receipt})
 if err!=nil||envelope.SchemaVersion!=2||hash!=digest(raw){t.Fatal("exact signed envelope",err)}
 _,again,_,err:=CompileSignedAssemblyEnvelope(assemblyRef,assemblyRaw,shards,closureRef,closureRaw,tenant,actor,parent,[]Slot{slot},[]SigningDependency{dependency},[]SignedDependencyReceipt{receipt})
 if err!=nil||!bytes.Equal(raw,again){t.Fatal("signed envelope replay",err)}
 envelopeRef:=v2input.Ref{ArtifactID:uuid.New(),SHA256:hash}
 roots,err:=SignedAssemblyRequiredRoots(assemblyRef,assemblyRaw,shards,closureRef,closureRaw,envelopeRef,tenant,actor,parent,[]Slot{slot},[]SigningDependency{dependency},[]SignedDependencyReceipt{receipt})
 if err!=nil||len(roots)!=8{t.Fatal("signed task roots",err)}
 inventory:=map[string]bool{}
 for _,entry:=range roots{inventory[entry.Contract]=true}
 for _,contract:=range []string{"SignedDeliveryAssemblyEnvelope","DeliveryAssemblyInput","SignedDeliveryAssemblyClosure","DeliverySlotShard","ExportPresetConfig","EncodedExport","SignedExport","C2PAManifest"}{
  if !inventory[contract]{t.Fatal("missing signed root",contract)}
 }
 wrong:=assembly;wrong.Policy.Provenance="off";wrong.Policy.SigningPolicyID=uuid.Nil;wrong.Policy.SigningPolicySHA=""
 wrongRaw,_:=canonical.Marshal(wrong)
 if _,_,_,err:=CompileSignedAssemblyEnvelope(v2input.Ref{ArtifactID:assemblyRef.ArtifactID,SHA256:digest(wrongRaw)},wrongRaw,shards,closureRef,closureRaw,tenant,actor,parent,[]Slot{slot},[]SigningDependency{dependency},[]SignedDependencyReceipt{receipt});err==nil{t.Fatal("off assembly accepted")}
 if _,_,_,err:=CompileSignedAssemblyEnvelope(assemblyRef,assemblyRaw,shards,assemblyRef,closureRaw,tenant,actor,parent,[]Slot{slot},[]SigningDependency{dependency},[]SignedDependencyReceipt{receipt});err==nil{t.Fatal("aliased documents")}
 corrupt:=append([]byte(nil),shard...);corrupt[len(corrupt)-1]='!'
 if _,_,_,err:=CompileSignedAssemblyEnvelope(assemblyRef,assemblyRaw,[][]byte{corrupt},closureRef,closureRaw,tenant,actor,parent,[]Slot{slot},[]SigningDependency{dependency},[]SignedDependencyReceipt{receipt});err==nil{t.Fatal("substituted shard accepted")}
 changed:=slot;other:=uuid.New();changed.ExportID=&other
 if _,_,_,err:=CompileSignedAssemblyEnvelope(assemblyRef,assemblyRaw,shards,closureRef,closureRaw,tenant,actor,parent,[]Slot{changed},[]SigningDependency{dependency},[]SignedDependencyReceipt{receipt});err==nil{t.Fatal("changed export accepted")}
}

func TestSignedAssemblyShardReplayRepeatsSlotIntegrity(t *testing.T) {
 sha:=strings.Repeat("a",64)
 preset:=uuid.New()
 exportID:=uuid.New()
 export:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}
 slots:=[]Slot{
  {Ordinal:0,Binding:v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},CanvasHash:sha},PresetID:preset,PresetSHA:sha,Required:false,State:"ready",ExportID:&exportID,Export:&export},
  {Ordinal:1,Binding:v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},CanvasHash:sha},PresetID:preset,PresetSHA:sha,Required:false,State:"ready",ExportID:&exportID,Export:&export},
 }
 omission:="valid omission"
 assembly:=AssemblyInput{SchemaVersion:1,SnapshotDigest:sha,ItemCount:2,SlotCount:2,
  Policy:Policy{OptionalPresets:[]uuid.UUID{preset},AllowOmitted:true},
  Presets:[]FrozenPreset{{PresetID:preset,PresetSHA:sha,Config:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}}},
  Shards:[]v2input.Ref{{ArtifactID:uuid.New()}}}
 check:=func(current []Slot)bool{
  raw,err:=canonical.Marshal(SlotShard{1,sha,current});if err!=nil{t.Fatal(err)}
  assembly.Shards[0].SHA256=digest(raw)
  return signedAssemblyShardsMatch(assembly,current,[][]byte{raw})
 }
 if check(slots){t.Fatal("duplicate export ID accepted on replay")}
 secondID:=uuid.New();secondExport:=v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}
 slots[1].ExportID=&secondID;slots[1].Export=&secondExport
 if !check(slots){t.Fatal("valid distinct exports rejected")}
 slots[1].State="omitted";slots[1].ExportID=nil;slots[1].Export=nil;slots[1].Reason=&omission
 if !check(slots){t.Fatal("valid omission rejected")}
 for _,reason:=range []string{"  ",strings.Repeat("x",201),string([]byte{0xff})}{
  slots[1].Reason=&reason
  if check(slots){t.Fatal("invalid omission accepted",reason)}
 }
}

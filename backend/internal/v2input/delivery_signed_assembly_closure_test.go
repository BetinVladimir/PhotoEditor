package v2input

import (
 "fmt"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)

func TestSignedDeliveryAssemblyClosurePinsBothDocumentsAndSignedChildren(t *testing.T){
 makeEntry:=func(contract string,n int)Entry{return Entry{Ref:Ref{ArtifactID:uuid.New(),SHA256:fmt.Sprintf("%064x",n)},Contract:contract}}
 roots:=[]Entry{
  makeEntry("SignedDeliveryAssemblyEnvelope",1),makeEntry("DeliveryAssemblyInput",2),
  makeEntry("SignedDeliveryAssemblyClosure",3),makeEntry("DeliverySlotShard",4),
  makeEntry("ExportPresetConfig",5),makeEntry("EncodedExport",6),
  makeEntry("SignedExport",7),makeEntry("C2PAManifest",8),
 }
 params:=DeliveryAssembleParams{SchemaVersion:1,Assembly:roots[0].Ref}
 sha,err:=canonical.Hash(params);if err!=nil{t.Fatal(err)}
 paramsRef:=Ref{ArtifactID:uuid.New(),SHA256:sha};workflow:=uuid.New()
 closure,err:=CompileSignedDeliveryAssemblyClosure(workflow,params,paramsRef,roots,roots)
 if err!=nil||len(closure.Direct)!=2||closure.Direct[1]!=roots[0].Ref{t.Fatal("signed closure",err)}
 reordered:=[]Entry{roots[7],roots[5],roots[2],roots[0],roots[6],roots[3],roots[4],roots[1]}
 replay,err:=CompileSignedDeliveryAssemblyClosure(workflow,params,paramsRef,roots,reordered)
 if err!=nil||replay.InputDigest!=closure.InputDigest{t.Fatal("signed replay",err)}
 for _,scenario:=range []string{"missing_signed","missing_manifest","wrong_root","duplicate","missing_closure","aliased_contract"}{
  currentRoots:=append([]Entry(nil),roots...);resolved:=append([]Entry(nil),roots...);currentParams:=params
  switch scenario{
  case "missing_signed":currentRoots=append(currentRoots[:6],currentRoots[7:]...);resolved=append(resolved[:6],resolved[7:]...)
  case "missing_manifest":currentRoots=currentRoots[:7];resolved=resolved[:7]
  case "wrong_root":currentParams.Assembly=roots[1].Ref
  case "duplicate":currentRoots=append(currentRoots,roots[0])
  case "missing_closure":resolved=append(resolved[:2],resolved[3:]...)
  case "aliased_contract":resolved[6].Contract="EncodedExport"
  }
  if _,err:=CompileSignedDeliveryAssemblyClosure(workflow,currentParams,paramsRef,currentRoots,resolved);err==nil{t.Fatal("accepted",scenario)}
 }
}

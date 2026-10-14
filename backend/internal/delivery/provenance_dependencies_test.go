package delivery

import (
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)

func TestCompileSigningDependenciesStableAndClosed(t *testing.T) {
 sha:=strings.Repeat("a",64)
 binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:sha},CanvasHash:sha}
 preset:=uuid.New();exportID:=uuid.New();artifact:=uuid.New()
 ready:=Slot{Ordinal:0,Binding:binding,PresetID:preset,PresetSHA:sha,Required:true,State:"ready",ExportID:&exportID,Export:&v2input.Ref{ArtifactID:artifact,SHA256:sha}}
 omittedReason:="OPTIONAL_FAILED"
 omitted:=Slot{Ordinal:1,Binding:binding,PresetID:uuid.New(),PresetSHA:sha,State:"omitted",Reason:&omittedReason}
 parent,policy:=uuid.New(),uuid.New()
 first,err:=CompileSigningDependencies(parent,policy,[]Slot{omitted,ready});if err!=nil||len(first)!=1{t.Fatal(first,err)}
 second,err:=CompileSigningDependencies(parent,policy,[]Slot{ready,omitted});if err!=nil||len(second)!=1||second[0]!=first[0]{t.Fatal("unstable dependency",err)}
 if first[0].ExportID!=exportID||first[0].Export.ArtifactID!=artifact||first[0].PolicyID!=policy||len(first[0].IdempotencyKey)!=36{t.Fatal("signing scope lost",first)}
 for _,mode:=range []string{"missing_policy","required_omitted","pending","duplicate_slot","duplicate_export","missing_ref"}{
  current:=[]Slot{ready,omitted};currentPolicy:=policy
  switch mode{
  case "missing_policy":currentPolicy=uuid.Nil
  case "required_omitted":current[1].Required=true
  case "pending":current[0].State="pending"
  case "duplicate_slot":current[1]=ready
  case "duplicate_export":other:=ready;other.PresetID=uuid.New();current[1]=other
  case "missing_ref":bad:=ready;bad.Export=&v2input.Ref{};current[0]=bad
  }
  if _,err:=CompileSigningDependencies(parent,currentPolicy,current);err==nil{t.Fatal("invalid signing plan accepted",mode)}
 }
}

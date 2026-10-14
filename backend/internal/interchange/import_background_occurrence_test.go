package interchange

import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func TestPreparedBackgroundOccurrenceBindsExactPinsAndTarget(t *testing.T){
 scope:=preparedPublisherScope();scope.Contract="StyleBackgroundAsset"
 pin:=ImportBackgroundSource{Style:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("style"))},Portable:"bg",Original:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("image"))},Staged:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("image"))}}
 scope.Background=&pin
 ref:=v2input.Ref{ArtifactID:importBackgroundID(scope.Workflow,pin,scope.Binding),SHA256:pin.Original.SHA256}
 if err:=validateImportBackgroundOccurrence(scope,ref);err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"missing","style","portable","original","staged_hash","target","workflow","ref","contract","warp"}{t.Run(mode,func(t *testing.T){
  candidate:=scope;owned:=pin;candidate.Background=&owned;changed:=ref
  switch mode{case "missing":candidate.Background=nil;case "style":owned.Style.ArtifactID=uuid.New();case "portable":owned.Portable=" bg ";case "original":owned.Original.ArtifactID=uuid.New();case "staged_hash":owned.Staged.SHA256=rawSHA([]byte("changed"));case "target":candidate.Binding.AssetVersionID=uuid.New();case "workflow":candidate.Workflow=uuid.New();case "ref":changed.ArtifactID=uuid.New();case "contract":candidate.Contract="StyleV2";case "warp":candidate.Warp=&ImportWarpOccurrence{}}
  if err:=validateImportBackgroundOccurrence(candidate,changed);err==nil{t.Fatal("accepted changed occurrence")}
 })}
}

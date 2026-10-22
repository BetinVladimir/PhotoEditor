package v2region

import (
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

func regionSubjectFixture(t *testing.T)review.MaskRef{
 t.Helper()
 hash,err:=canonical.Hash(map[string]any{"steps":[]any{}});if err!=nil{t.Fatal(err)}
 chain,err:=canonical.Marshal(map[string]any{"hash":hash,"steps":[]any{}});if err!=nil{t.Fatal(err)}
 return review.MaskRef{Artifact:review.ArtifactRef{ID:uuid.New(),SHA256:regionSHA},Canvas:review.Canvas{Width:2,Height:3,Orientation:1,SpaceID:"source",SourceSHA256:regionSHA},TransformChain:json.RawMessage(chain),Selector:"subject",Confidence:.9}
}

func TestRegionBackgroundClosureTypesDecodedAssetAndSubject(t *testing.T){
 image:=v2input.Ref{ArtifactID:uuid.New(),SHA256:regionSHA};background:=v2input.Ref{ArtifactID:uuid.New(),SHA256:regionSHA};mask:=regionSubjectFixture(t)
 raw,err:=canonical.Marshal(map[string]any{"image":image,"background":background,"mode":"asset","subject":mask});if err!=nil{t.Fatal(err)}
 refs,err:=regionExecutionChildren(raw,v2input.Entry{Ref:image,Contract:"Params"});if err!=nil{t.Fatal(err)}
 found:=map[v2input.Ref]string{};for _,ref:=range refs{found[ref.Ref]=ref.Contract}
 if len(found)!=3||found[image]!="WorkingImage"||found[background]!="WorkingImage"||found[v2input.Ref{ArtifactID:mask.Artifact.ID,SHA256:mask.Artifact.SHA256}]!="NpyArray"{t.Fatal(found)}
}

func TestRegionBackgroundClosureRejectsUnknownAndProtectedSubject(t *testing.T){
 image:=v2input.Ref{ArtifactID:uuid.New(),SHA256:regionSHA}
 for _,kind:=range []string{"unknown","protected"}{
  mask:=regionSubjectFixture(t);if kind=="protected"{mask.Protected=true}
  value:=map[string]any{"image":image,"mode":"transparent","subject":mask};if kind=="unknown"{value["arbitrary_asset"]=image}
  raw,err:=canonical.Marshal(value);if err!=nil{t.Fatal(err)}
  if _,err=regionExecutionChildren(raw,v2input.Entry{Ref:image,Contract:"Params"});err==nil{t.Fatal("accepted invalid background",kind)}
 }
}

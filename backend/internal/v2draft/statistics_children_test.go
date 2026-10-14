package v2draft
import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestStatsParamsTypedPersonDependencies(t *testing.T){
 source:=v2input.Ref{ArtifactID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
 canvas:=review.Canvas{Width:2,Height:3,Orientation:1,SpaceID:"source",SourceSHA256:source.SHA256}
 steps:=[]any{};hash,err:=canonical.Hash(map[string]any{"steps":steps});if err!=nil{t.Fatal(err)}
 chain,err:=canonical.Marshal(map[string]any{"steps":steps,"hash":hash});if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"valid","selector","extra","null"}{t.Run(mode,func(t *testing.T){
  mask:=review.MaskRef{Artifact:review.ArtifactRef{ID:uuid.New(),SHA256:source.SHA256},Canvas:canvas,TransformChain:chain,Selector:"person",Confidence:1}
  if mode=="selector"{mask.Selector="skin"}
  value:=map[string]any{"image":source,"person":mask};if mode=="extra"{value["extra"]=true};if mode=="null"{value["person"]=nil}
  raw,err:=canonical.Marshal(value);if err!=nil{t.Fatal(err)}
  parent:=v2input.Ref{ArtifactID:uuid.New(),SHA256:"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
  children,err:=DocumentChildren(raw,"Params",parent)
  if mode!="valid"{if err==nil{t.Fatal("invalid stats Params accepted")};return}
  if err!=nil||len(children)!=2||children[0].Contract!="WorkingImage"||children[1].Contract!="MaskPixels"||children[1].Path!="Document.person.artifact"||children[0].CanvasHash==""{t.Fatal(children,err)}
 })}
}

func TestStatsParamsExtractorDoesNotClaimOtherPersonConsumers(t *testing.T){
 parent:=v2input.Ref{ArtifactID:uuid.New(),SHA256:"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}
 for _,field:=range []string{"poses","base_aligned"}{t.Run(field,func(t *testing.T){
  // Malformed masks deliberately prove that the stats parser is not selected;
  // each owning operation must perform its own complete schema validation.
  raw,err:=canonical.Marshal(map[string]any{"image":parent,"person":nil,field:parent});if err!=nil{t.Fatal(err)}
  children,handled,err:=statsParamRefs(raw,"Document",parent)
  if err!=nil||handled||children!=nil{t.Fatal("foreign person consumer claimed as stats",handled,err)}
 })}
}

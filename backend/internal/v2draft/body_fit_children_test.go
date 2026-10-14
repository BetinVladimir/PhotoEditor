package v2draft
import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestBodyFitPairedDependenciesKeepSideGeometry(t *testing.T){
 ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}
 hash,err:=canonical.Hash(map[string]any{"steps":[]any{}});if err!=nil{t.Fatal(err)}
 chain,err:=canonical.Marshal(map[string]any{"steps":[]any{},"hash":hash});if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"params","result","faces","unpaired_faces","mask_selector","missing_pose","pair","unknown"}{t.Run(mode,func(t *testing.T){
  before:=review.MaskRef{Artifact:review.ArtifactRef{ID:uuid.New(),SHA256:ref().SHA256},Canvas:review.Canvas{Width:2,Height:3,Orientation:1,SpaceID:"source",SourceSHA256:ref().SHA256},TransformChain:chain,Selector:"person",Confidence:1}
  after:=before;after.Artifact.ID=uuid.New();after.Canvas.Width=4
  if mode=="mask_selector"{after.Selector="manual"}
  value:=map[string]any{"pair_id":uuid.New(),"before":ref(),"after":ref(),"alignment":ref(),"before_pose":ref(),"after_pose":ref(),"before_person":before,"after_person":after}
  if mode=="faces"||mode=="unpaired_faces"{value["before_faces"]=ref()};if mode=="faces"{value["after_faces"]=ref()}
  if mode=="missing_pose"{value["after_pose"]=nil};if mode=="pair"{value["pair_id"]=uuid.Nil};if mode=="unknown"{value["approved"]=true}
  contract:="Params";path:="Document";document:=any(value)
  if mode=="result"{contract="BodyFit";path="Document.params";document=map[string]any{"params":value,"objects":[]any{}}}
  raw,err:=canonical.Marshal(document);if err!=nil{t.Fatal(err)};parent:=ref()
  refs,err:=DocumentChildren(raw,contract,parent)
  if mode!="params"&&mode!="result"&&mode!="faces"{if err==nil{t.Fatal("invalid paired dependency projection accepted")};return}
  expected:=7;if mode=="faces"{expected=9}
  if err!=nil||len(refs)!=expected||refs[0].Contract!="WorkingImage"||refs[1].Contract!="WorkingImage"||refs[2].Contract!="PairAlignment"||refs[3].Contract!="PoseSet"||refs[5].Contract!="MaskPixels"{t.Fatal(refs,err)}
  if refs[0].CanvasHash==refs[1].CanvasHash||refs[3].CanvasHash!=refs[0].CanvasHash||refs[4].CanvasHash!=refs[1].CanvasHash||refs[5].CanvasHash!=refs[0].CanvasHash||refs[6].CanvasHash!=refs[1].CanvasHash{t.Fatal("pair side geometry lost")}
  if refs[0].Path!=path+".before"||refs[6].Path!=path+".after_person.artifact"{t.Fatal("typed paths lost")}
  for _,child:=range refs{if child.ParentRef!=parent{t.Fatal("fit parent lost")}}
 })}
}

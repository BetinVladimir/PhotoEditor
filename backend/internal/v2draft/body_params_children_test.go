package v2draft
import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestBodyParamsTypedDependencies(t *testing.T){
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
 hash,err:=canonical.Hash(map[string]any{"steps":[]any{}});if err!=nil{t.Fatal(err)}
 chain,err:=canonical.Marshal(map[string]any{"steps":[]any{},"hash":hash});if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"coefficients","fit","both","neither","poses_null","mask_selector","unknown","faces"}{t.Run(mode,func(t *testing.T){
  mask:=review.MaskRef{Artifact:review.ArtifactRef{ID:uuid.New(),SHA256:ref.SHA256},Canvas:review.Canvas{Width:2,Height:3,Orientation:1,SpaceID:"source",SourceSHA256:ref.SHA256},TransformChain:chain,Selector:"person",Confidence:1}
  if mode=="mask_selector"{mask.Selector="manual"}
  value:=map[string]any{"image":ref,"poses":ref,"person":mask,"coefficients":map[string]any{"face_slim":0,"waist_slim":0,"hip_slim":0,"torso_slim":0,"leg_length":0}}
  if mode=="fit"||mode=="both"{value["fit"]=ref};if mode=="fit"||mode=="neither"{delete(value,"coefficients")}
  if mode=="faces"{value["faces"]=ref}
  if mode=="poses_null"{value["poses"]=nil};if mode=="unknown"{value["approved"]=true}
  raw,err:=canonical.Marshal(value);if err!=nil{t.Fatal(err)}
  refs,handled,err:=bodyParamRefs(raw,"Document",ref)
  if mode!="coefficients"&&mode!="fit"&&mode!="faces"{if err==nil||!handled||refs!=nil{t.Fatal("invalid body projection accepted",err)};return}
  expected:=3;if mode=="fit"||mode=="faces"{expected=4}
  if err!=nil||!handled||len(refs)!=expected||refs[0].Contract!="WorkingImage"||refs[1].Contract!="PoseSet"||refs[2].Contract!="MaskPixels"{t.Fatal("body dependencies",refs,err)}
  if mode=="faces"&&refs[3].Contract!="FaceLandmarks"{t.Fatal("body face landmarks contract lost")}
  if mode=="fit"&&refs[3].Contract!="BodyFit"{t.Fatal("fit contract lost")}
 })}
}

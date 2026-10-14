package interchange
import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestExportBodyRequiredInputsPreserveWorkerPins(t *testing.T){
 _,source,imageRaw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)}
 for _,mode:=range []string{"coefficients","fit_faces","bad_hash","bad_pose","both_sources"}{t.Run(mode,func(t *testing.T){
  poses:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("poses"))};fit:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("fit"))};faces:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("faces"))}
  mask:=review.MaskRef{Artifact:image.Pixels,Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"person",Confidence:1}
  value:=map[string]any{"image":source.Ref,"poses":poses,"person":mask,"coefficients":map[string]any{"face_slim":0,"waist_slim":0,"hip_slim":0,"torso_slim":0,"leg_length":0}}
  if mode=="fit_faces"{delete(value,"coefficients");value["fit"]=fit;value["faces"]=faces}
  if mode=="bad_pose"{value["poses"]=nil};if mode=="both_sources"{value["fit"]=fit}
  raw,err:=canonical.Marshal(value);if err!=nil{t.Fatal(err)}
  entry:=v2input.Entry{Contract:"Params",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}}
  if mode=="bad_hash"{entry.Ref.SHA256=rawSHA([]byte("foreign"))}
  refs,err:=exportBodyRequiredInputs(entry,raw)
  if mode!="coefficients"&&mode!="fit_faces"{if err==nil||refs!=nil{t.Fatal("invalid body inventory accepted",err)};return}
  expected:=[]v2input.Ref{source.Ref,poses,{ArtifactID:mask.Artifact.ID,SHA256:mask.Artifact.SHA256}}
  if mode=="fit_faces"{expected=append(expected,fit,faces)}
  if err!=nil||len(refs)!=len(expected){t.Fatal(refs,err)};for i:=range expected{if refs[i]!=expected[i]{t.Fatal("body pin/order changed",i)}}
 })}
}

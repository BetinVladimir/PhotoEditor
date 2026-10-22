package interchange
import (
 "encoding/json"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestExportReportMasksRequireClosedReportAndOwnBytes(t *testing.T){
 _,source,rawImage:=importWorkingValidatorFixture(t)
 image,err:=review.ParseWorkingImage(rawImage);if err!=nil{t.Fatal(err)}
 ref:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("artifact"))}}
 mask:=map[string]any{"artifact":ref(),"canvas":image.Canvas,"transform_chain":json.RawMessage(image.TransformChain),"object_id":nil,"model_lock_id":nil,"selector":"person","confidence":1,"protected":true}
 for _,mode:=range []string{"valid","algorithm","hash","contract"}{t.Run(mode,func(t *testing.T){
  report:=map[string]any{"params":map[string]any{"image":source.Ref,"faces":[]any{},"permanent":nil,"diameter_face_fraction":[]float64{.002,.012},"protections":[]any{}},"proposal":ref(),"protection":mask,"label_png":ref(),"faces":[]any{},"warnings":[]string{"no_faces"},"algorithm":"defect_dog_v1"}
  if mode=="algorithm"{report["algorithm"]="untrusted"}
  raw,err:=canonical.Marshal(report);if err!=nil{t.Fatal(err)}
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"DefectProposeReport"}
  if mode=="hash"{entry.Ref.SHA256=rawSHA([]byte("foreign"))};if mode=="contract"{entry.Contract="Params"}
  masks,err:=exportReportInlineMasks(entry,raw)
  if mode!="valid"{if err==nil{t.Fatal("unqualified report masks extracted")};return}
  if err!=nil||len(masks)!=1{t.Fatal("report protection missing",err)}
  digest:=rawSHA(raw);masks[0][0]='!';if rawSHA(raw)!=digest{t.Fatal("mask aliases report")}
 })}
}

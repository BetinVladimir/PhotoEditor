package workflow

import (
 "context"
 "encoding/json"
 "os"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
)

func editorSealFixture(t *testing.T)(editorSealOrigin,[]styleArtifact,deliveryStageObjects){
 t.Helper();raw,err:=os.ReadFile("../review/testdata/working-images-python.json");if err!=nil{t.Fatal(err)};var images []json.RawMessage;if json.Unmarshal(raw,&images)!=nil||len(images)==0{t.Fatal("fixture")};imageRaw,err:=canonical.Bytes(images[0]);if err!=nil{t.Fatal(err)};image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{t.Fatal(err)};canvas,_:=canonical.Hash(image.Canvas)
 objects:=deliveryStageObjects{};inventory:=[]styleArtifact{}
 add:=func(body []byte)review.ArtifactRef{id:=uuid.New();sha:=renderDerivativeBytesSHA(body);key:=id.String();objects[key]=body;inventory=append(inventory,styleArtifact{ArtifactID:id,SHA256:sha,Bytes:int64(len(body)),Bucket:"photos",Key:key,VersionID:"immutable-1",MediaType:"application/json"});return review.ArtifactRef{ID:id,SHA256:sha}}
 source:=add(imageRaw);recipe:=add([]byte(`{}`));style:=add([]byte(`{}`));qualityBody,_:=canonical.Marshal(map[string]any{"target":source,"target_kind":"working","stage":"master","threshold_profile_id":"master_v1","status":"pass","technical":map[string]any{"finite":true,"below_zero_fraction":0,"above_one_fraction":0,"clipped_black_fraction":0,"clipped_white_fraction":0,"sharpness_laplacian":1,"seam_p99_delta":0},"reference":nil,"reasons":[]string{}});quality:=add(qualityBody)
 return editorSealOrigin{Run:uuid.New(),AssetVersion:uuid.New(),SourceID:source.ID,SourceSHA:source.SHA256,SourceCanvas:canvas,ProcessedID:source.ID,ProcessedSHA:source.SHA256,RecipeID:recipe.ID,RecipeSHA:recipe.SHA256,StyleID:style.ID,StyleSHA:style.SHA256,QualityID:quality.ID,QualitySHA:quality.SHA256,CompilationSHA:strings.Repeat("a",64)},inventory,objects
}
func TestEditorSealPlanRetainsOriginalSourceAndStandaloneCPUProfile(t *testing.T){
 origin,inventory,objects:=editorSealFixture(t);body,_:=canonical.Marshal(origin);refs,_:=canonical.Marshal(inventory)
 doc,err:=buildEditorSealDocument(context.Background(),objects,"photos",body,[]byte(`[]`),refs);if err!=nil{t.Fatal(err)}
 var params map[string]json.RawMessage;json.Unmarshal(doc.Params,&params);if string(params["generated"])!="null"||string(params["generative_requested"])!="false"||params["coverage_trace"]!=nil{t.Fatal("invented generation trace")}
 var profile map[string]json.RawMessage;json.Unmarshal(doc.Profile,&profile);if string(profile["backend"])!=`"cpu"`||profile["renderer_version"]!=nil{t.Fatal("seal incorrectly requires render input manifest")}
 if doc.SHA!=renderDerivativeBytesSHA(doc.Params)||len(doc.Inputs)!=len(inventory)||doc.CanvasHash!=origin.SourceCanvas{t.Fatal("pins changed")}
}
func TestEditorSealPlanRejectsPhysicalMutationAndDiscontinuousTrace(t *testing.T){
 origin,inventory,objects:=editorSealFixture(t);body,_:=canonical.Marshal(origin);refs,_:=canonical.Marshal(inventory)
 key:=inventory[0].Key;saved:=objects[key];objects[key]=append(append([]byte(nil),saved...),' ')
 if _,err:=buildEditorSealDocument(context.Background(),objects,"photos",body,[]byte(`[]`),refs);err==nil{t.Fatal("changed source bytes accepted")};objects[key]=saved
 source:=review.ArtifactRef{ID:origin.SourceID,SHA256:origin.SourceSHA};trace,_:=canonical.Marshal([]editorCoverageStep{{TaskID:uuid.New(),ResultID:uuid.New(),Attempt:1,Fence:1,Operation:"image.generate",Source:review.ArtifactRef{ID:uuid.New(),SHA256:source.SHA256},Output:source}})
 if _,err:=buildEditorSealDocument(context.Background(),objects,"photos",body,trace,refs);err==nil{t.Fatal("discontinuous source trace accepted")}
 trace,_=canonical.Marshal([]editorCoverageStep{{TaskID:uuid.New(),ResultID:uuid.New(),Attempt:1,Fence:1,Operation:"image.generate",Source:source,Output:source}})
 if _,err:=buildEditorSealDocument(context.Background(),objects,"photos",body,trace,refs);err==nil{t.Fatal("generation without canonical report accepted")}
}

package interchange
import (
 "context"
 "encoding/json"
 "os"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func importWorkingValidatorFixture(t *testing.T)(ImportScopedDocumentValidator,v2input.Entry,[]byte){
 raw,err:=os.ReadFile("../review/testdata/working-images-python.json");if err!=nil{t.Fatal(err)}
 var images []json.RawMessage;if err:=json.Unmarshal(raw,&images);err!=nil{t.Fatal(err)}
 body,err:=canonical.Bytes(images[0]);if err!=nil{t.Fatal(err)};image,err:=review.ParseWorkingImage(body);if err!=nil{t.Fatal(err)}
 hash,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)}
 validator:=ImportScopedDocumentValidator{masters:map[uuid.UUID]importWorkingEvidence{ref.ArtifactID:{ref:ref,canvas:hash,original:image.Canvas.SourceSHA256}}}
 return validator,v2input.Entry{Ref:ref,Contract:"WorkingImage"},body
}
func TestImportWorkingValidatorUsesExistingImageContractAndBinaryRoles(t *testing.T){
 validator,entry,body:=importWorkingValidatorFixture(t);children,err:=validator.ImportDocumentChildren(context.Background(),entry,body)
 if err!=nil||len(children)<1{t.Fatal("working image",err)}
 found:=false;for _,child:=range children{if child.Contract=="WorkingPixels"{found=true}};if !found{t.Fatal("pixel role missing")}
}
func TestImportWorkingValidatorRejectsForeignOriginalOrCanvas(t *testing.T){
 for _,field:=range []string{"original","canvas"}{
  validator,entry,body:=importWorkingValidatorFixture(t);evidence:=validator.masters[entry.Ref.ArtifactID]
  if field=="original"{evidence.original=rawSHA([]byte("foreign"))}else{evidence.canvas=rawSHA([]byte("foreign"))};validator.masters[entry.Ref.ArtifactID]=evidence
  if _,err:=validator.ImportDocumentChildren(context.Background(),entry,body);err==nil{t.Fatal("foreign source accepted",field)}
 }
}

func TestImportWorkingValidatorRejectsNoncanonicalDType(t *testing.T){
 validator,entry,body:=importWorkingValidatorFixture(t)
 var wire map[string]any;if err:=json.Unmarshal(body,&wire);err!=nil{t.Fatal(err)};wire["dtype"]="uint16"
 body,err:=canonical.Marshal(wire);if err!=nil{t.Fatal(err)}
 entry.Ref.SHA256=rawSHA(body);evidence:=validator.masters[entry.Ref.ArtifactID];evidence.ref=entry.Ref;validator.masters[entry.Ref.ArtifactID]=evidence
 if _,err:=validator.ImportDocumentChildren(context.Background(),entry,body);err==nil{t.Fatal("non-float canonical ingest source accepted")}
}

func TestImportScopedValidatorPreservesParsedMetadata(t *testing.T){
 for _,change:=range []string{"none","rating","pick","crop","missing_crop"}{
  parsed:=importParsedValidatorFixture();parsed.Manifest.Entries[0].Crop=&importCrop{X0:0.1,Y0:0.2,X1:0.8,Y1:0.9}
  original:=parsed.Manifest.Entries[0];project:=uuid.New();snapshot:=rawSHA([]byte("snapshot"));crop:=*original.Crop
  item:=importPlanEntry{ID:original.ID,External:original.External,Path:original.ImagePath,Content:parsed.Files[0].Content,Rating:original.Rating,Pick:original.Pick,Crop:&crop}
  switch change{case "rating":item.Rating=5;case "pick":item.Pick="picked";case "crop":item.Crop.X0=0.3;case "missing_crop":item.Crop=nil}
  plan:=importPlanDocument{Schema:1,Project:project,Direction:"import",Format:"photoeditor_bundle",Snapshot:snapshot,Entries:[]importPlanEntry{item},Archive:1024,Expanded:2048,Files:10}
  planRaw,err:=canonical.Marshal(plan);if err!=nil{t.Fatal(err)};parsedRaw,err:=canonical.Marshal(parsed);if err!=nil{t.Fatal(err)}
  _,err=NewImportScopedDocumentValidator(project,snapshot,planRaw,parsedRaw)
  if change=="none"&&err!=nil{t.Fatal("unchanged metadata refused",err)}
  if change!="none"&&err==nil{t.Fatal("modified parsed metadata accepted",change)}
 }
}

func TestImportWorkingPreservesDenseWarpRole(t *testing.T){
 for _,mode:=range []string{"valid","pixel_alias"}{t.Run(mode,func(t *testing.T){
  validator,entry,raw:=importWorkingValidatorFixture(t)
  image,err:=review.ParseWorkingImage(raw);if err!=nil{t.Fatal(err)}
  field:=review.ArtifactRef{ID:uuid.New(),SHA256:rawSHA([]byte("warp"))}
  if mode=="pixel_alias"{field=image.Pixels}
  steps:=[]any{map[string]any{"kind":"dense_warp","from_canvas":image.Canvas,"to_canvas":image.Canvas,"invertibility":"partial","valid_region":map[string]any{"x":0,"y":0,"width":image.Canvas.Width,"height":image.Canvas.Height},"inverse_field":field}}
  hash,err:=canonical.Hash(map[string]any{"steps":steps});if err!=nil{t.Fatal(err)}
  image.TransformChain,err=canonical.Marshal(map[string]any{"steps":steps,"hash":hash});if err!=nil{t.Fatal(err)}
  raw,err=canonical.Marshal(image);if err!=nil{t.Fatal(err)}
  entry.Ref.SHA256=rawSHA(raw);evidence:=validator.masters[entry.Ref.ArtifactID];evidence.ref=entry.Ref;validator.masters[entry.Ref.ArtifactID]=evidence
  children,err:=validator.ImportDocumentChildren(context.Background(),entry,raw)
  if mode!="valid"{if err==nil{t.Fatal("pixel/warp alias accepted")};return}
  if err!=nil{t.Fatal(err)}
  found:=false;for _,child:=range children{if child.Ref.ArtifactID==field.ID{if child.Contract!="DenseWarpField"||child.Ref.SHA256!=field.SHA256{t.Fatal("warp role lost")};found=true}}
  if !found{t.Fatal("warp omitted")}
 })}
}

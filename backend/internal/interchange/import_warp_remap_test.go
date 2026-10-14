package interchange

import (
 "context"
 "math"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestImportedDenseWarpUsesOccurrenceCanvasInsteadOfFinalImageSize(t *testing.T){
 for _,change:=range []string{"none","shape","nan","foreign_source","version","receipt","missing_inventory"}{t.Run(change,func(t *testing.T){
  _,_,raw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(raw);if err!=nil{t.Fatal(err)}
  image.Canvas.Width=3;image.Canvas.Height=2
  hash,err:=canonical.Hash(map[string]any{"steps":[]any{}});if err!=nil{t.Fatal(err)}
  image.TransformChain,err=canonical.Marshal(map[string]any{"steps":[]any{},"hash":hash});if err!=nil{t.Fatal(err)}
  master,err:=canonical.Marshal(image);if err!=nil{t.Fatal(err)};canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  target:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(master)},CanvasHash:canvas}
  from,to:=image.Canvas,image.Canvas;to.Width=2
  shape:="(2,2,2)";if change=="shape"{shape="(2,3,2)"}
  values:=[]float32{-10,20,0,1,2,3,4,5};if change=="nan"{values[0]=float32(math.NaN())}
  array:=importMaskArrayFixture(shape,values);ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(array)};staged:=uuid.New()
  parsed:=importParsedValidatorFixture();parsed.Manifest.Entries[0].ImageSHA=image.Canvas.SourceSHA256;parsed.Manifest.Inventory[0].SHA=image.Canvas.SourceSHA256;parsed.Files[0].Content.SHA256=image.Canvas.SourceSHA256
  parsed.Manifest.Inventory=append(parsed.Manifest.Inventory,importBundleInventory{ID:ref.ArtifactID,Path:"warp.npy",SHA:ref.SHA256,Contract:"DenseWarpField"})
  parsed.Files=append(parsed.Files,importParsedFile{Path:"warp.npy",Content:v2input.Ref{ArtifactID:staged,SHA256:ref.SHA256},Contract:"DenseWarpField"})
  parsedRaw,err:=canonical.Marshal(parsed);if err!=nil{t.Fatal(err)}
  catalog:=&importCatalogStub{object:objectstore.Object{Key:"warp",VersionID:"v1",ContentType:"application/octet-stream",SHA256:ref.SHA256,Bytes:int64(len(array))},change:change=="version"}
  objects:=&importReaderStub{body:array};masters:=&importMasterReaderStub{body:master};publisher:=&importBinaryPublisherStub{wrong:change=="receipt"}
  remapper,err:=NewImportedDenseWarpRemapper(uuid.New(),parsed.Manifest.Entries[0].ID,parsedRaw,catalog,objects,masters,publisher,1<<20);if err!=nil{t.Fatal(err)}
  if change=="foreign_source"{to.SourceSHA256=rawSHA([]byte("foreign"))};if change=="missing_inventory"{ref.ArtifactID=uuid.New()}
  result,err:=remapper.RemapImportedDenseWarp(context.Background(),ref,from,to,target)
  if change!="none"{if err==nil{t.Fatal("invalid warp accepted")};if change!="receipt"&&publisher.calls!=0{t.Fatal("warp published before physical verification")};return}
  if err!=nil||result.ArtifactID==ref.ArtifactID||result.SHA256!=ref.SHA256||catalog.calls!=3||masters.calls!=2{t.Fatal("occurrence-shaped field rejected",err)}
  again,err:=remapper.RemapImportedDenseWarp(context.Background(),ref,from,to,target);if err!=nil||again!=result{t.Fatal("occurrence replay changed identity",err)}
 })}
}

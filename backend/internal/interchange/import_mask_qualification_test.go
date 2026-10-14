package interchange

import (
 "context"
 "errors"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestPhysicalImportedMaskChecksArrayModelAndCurrentSource(t *testing.T){
 for _,change:=range []string{"none","person","model_missing","model_unknown","canvas","range","version","budget","cancelled"}{t.Run(change,func(t *testing.T){
  _,_,raw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(raw);if err!=nil{t.Fatal(err)}
  image.Canvas.Width=3;image.Canvas.Height=2
  chainHash,err:=canonical.Hash(map[string]any{"steps":[]any{}});if err!=nil{t.Fatal(err)}
  image.TransformChain,err=canonical.Marshal(map[string]any{"steps":[]any{},"hash":chainHash});if err!=nil{t.Fatal(err)}
  master,err:=canonical.Marshal(image);if err!=nil{t.Fatal(err)};canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  values:=[]float32{0,.2,.4,.6,.8,1};if change=="range"{values[0]=1.1}
  array:=importMaskArrayFixture("(2,3)",values);ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(array)}
  mask:=review.MaskRef{Artifact:review.ArtifactRef{ID:ref.ArtifactID,SHA256:ref.SHA256},Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"manual",Confidence:1,Protected:true}
  lock:=rawSHA([]byte("installed model"));locks:=[]string{lock}
  if change=="person"||change=="model_missing"||change=="model_unknown"{mask.Selector="person";if change!="model_missing"{mask.ModelLockID=&lock};if change=="model_unknown"{locks=nil}}
  binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(master)},CanvasHash:canvas};if change=="canvas"{binding.CanvasHash=rawSHA([]byte("foreign"))}
  catalog:=&importCatalogStub{object:objectstore.Object{Key:"mask",VersionID:"v1",ContentType:"application/x-npy",SHA256:ref.SHA256,Bytes:int64(len(array))},change:change=="version"}
  masters:=&importMasterReaderStub{body:master};objects:=&importReaderStub{body:array}
  budget:=int64(1<<20);if change=="budget"{budget=int64(len(array)-1)}
  qualifier,err:=NewImportPhysicalMaskQualifier(masters,catalog,objects,locks,budget);if err!=nil{t.Fatal(err)}
  // Installed policy is owned independently of its caller's slice.
  if len(locks)>0{locks[0]=rawSHA([]byte("changed caller policy"))}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if change=="cancelled"{cancel()}
  err=qualifier.QualifyImportedMask(ctx,mask,binding)
  if change=="none"||change=="person"{if err!=nil||objects.opens!=1||catalog.calls!=2||masters.calls!=2{t.Fatal("physical authority not checked",err)};return}
  if err==nil{t.Fatal("invalid imported mask accepted")}
  if change=="cancelled"&&!errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}
  if (change=="model_missing"||change=="model_unknown")&&objects.opens!=0{t.Fatal("unqualified model opened arrays")}
 })}
}

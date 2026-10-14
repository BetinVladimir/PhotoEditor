package interchange

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

func TestExportTypedArrayGeometryParentAndChildBinding(t *testing.T){
 for _,contract:=range []string{"WorkingPixels","WorkingAlpha","DenseWarpField"}{
 for _,mode:=range []string{"valid","foreign","child","hash","authority","cancelled"}{t.Run(contract+"/"+mode,func(t *testing.T){
  _,_,raw:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(raw);if err!=nil{t.Fatal(err)}
  ref:=review.ArtifactRef{ID:uuid.New(),SHA256:rawSHA([]byte("array"))}
  chainSteps:=[]any{}
  if contract=="WorkingPixels"{image.Pixels=ref}
  if contract=="WorkingAlpha"{image.Alpha=&ref}
  if contract=="DenseWarpField"{
   chainSteps=append(chainSteps,map[string]any{"kind":"dense_warp","from_canvas":image.Canvas,"to_canvas":image.Canvas,"invertibility":"partial","valid_region":map[string]any{"x":0,"y":0,"width":image.Canvas.Width,"height":image.Canvas.Height},"inverse_field":ref})
  }
  hash,err:=canonical.Hash(map[string]any{"steps":chainSteps});if err!=nil{t.Fatal(err)}
  image.TransformChain,err=canonical.Marshal(map[string]any{"steps":chainSteps,"hash":hash});if err!=nil{t.Fatal(err)}
  raw,err=canonical.Marshal(image);if err!=nil{t.Fatal(err)}
  parent:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"WorkingImage"}
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:ref.ID,SHA256:ref.SHA256},Contract:contract}
  stub:=&exportMaskNodeStub{node:ExportClosureNode{Entry:parent,Body:raw,Children:[]v2input.Entry{entry}}}
  parents:=[]v2input.Entry{parent};source,err:=NewExportTypedArrayGeometrySource(stub,parents);if err!=nil{t.Fatal(err)}
  parents[0].Contract="mutated"
  cause:=errors.New("revoked");ctx:=context.Background()
  switch mode{case "foreign":entry.Ref.ArtifactID=uuid.New();case "child":stub.node.Children=nil;case "hash":stub.node.Body=[]byte("{}");case "authority":stub.err=cause;case "cancelled":var cancel context.CancelFunc;ctx,cancel=context.WithCancel(ctx);cancel()}
  got,err:=source.ResolveExportArrayGeometry(ctx,entry)
  if (err==nil)!=(mode=="valid"){t.Fatalf("unexpected geometry: %v",err)}
  if mode=="valid"{canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)};dtype:=image.DType;if contract!="WorkingPixels"{dtype="float32"};if got.CanvasSHA!=canvas||got.DType!=dtype||int64(got.Width)!=image.Canvas.Width||int64(got.Height)!=image.Canvas.Height{t.Fatal("wrong typed geometry")}}
  if mode=="authority"&&!errors.Is(err,cause){t.Fatal("authority cause lost")}
  if mode=="cancelled"&&stub.calls!=0{t.Fatal("cancelled read performed IO")}
 })}}
}

func TestExportTypedArrayGeometryRejectsDuplicateOrUnknownParents(t *testing.T){
 parent:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("working"))},Contract:"WorkingImage"}
 conflict:=parent;conflict.Ref.SHA256=rawSHA([]byte("other"))
 for _,parents:=range [][]v2input.Entry{nil,{parent,parent},{parent,conflict},{{Ref:parent.Ref,Contract:"Artifact"}}}{
  if _,err:=NewExportTypedArrayGeometrySource(&exportMaskNodeStub{},parents);err==nil{t.Fatal("invalid parents accepted")}
 }
 if _,err:=NewExportTypedArrayGeometrySource(nil,[]v2input.Entry{parent});err==nil{t.Fatal("missing resolver accepted")}
}

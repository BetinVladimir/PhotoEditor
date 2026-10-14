package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestImportWarpGeometryUsesExactWitnessedTarget(t *testing.T){
 for _,mode:=range []string{"valid","reused","missing_witness","foreign","wrong_role","catalog_changed","cancelled"}{t.Run(mode,func(t *testing.T){
  _,_,raw:=importWorkingValidatorFixture(t)
  image,err:=review.ParseWorkingImage(raw);if err!=nil{t.Fatal(err)}
  field:=review.ArtifactRef{ID:uuid.New(),SHA256:rawSHA([]byte("warp"))}
  entry:=v2input.Entry{Contract:"DenseWarpField",Ref:v2input.Ref{ArtifactID:field.ID,SHA256:field.SHA256}}
  step:=map[string]any{"kind":"dense_warp","from_canvas":image.Canvas,"to_canvas":image.Canvas,"invertibility":"partial","valid_region":map[string]any{"x":0,"y":0,"width":image.Canvas.Width,"height":image.Canvas.Height},"inverse_field":field}
  steps:=[]any{step};if mode=="reused"{steps=append(steps,step)}
  hash,err:=canonical.Hash(map[string]any{"steps":steps});if err!=nil{t.Fatal(err)}
  image.TransformChain,err=canonical.Marshal(map[string]any{"steps":steps,"hash":hash});if err!=nil{t.Fatal(err)}
  raw,err=canonical.Marshal(image);if err!=nil{t.Fatal(err)}
  master:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}
  witnesses:=&ImportWorkingWitnesses{}
  if mode!="missing_witness"{if err:=witnesses.remember(master,raw,[]v2input.Entry{entry});err!=nil{t.Fatal(err)}}
  _,catalog,_:=importOpaqueFixture()
  source:=ImportCanonicalWarpGeometry{Witnesses:witnesses,Catalog:catalog}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  switch mode{case "foreign":entry.Ref.ArtifactID=uuid.New();case "wrong_role":entry.Contract="WorkingPixels";case "catalog_changed":catalog.change=true;case "cancelled":cancel()}
  result,err:=source.ResolveExportArrayGeometry(ctx,entry)
  valid:=mode=="valid"||mode=="reused"||mode=="catalog_changed"
  // Catalogue replacement is detected by the reader's immutable before/after
  // object comparison, not this single current-authority geometry lookup.
  if (err==nil)!=valid{t.Fatal("unexpected geometry decision",err)}
  if valid{canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)};if result.CanvasSHA!=canvas||result.Width!=int(image.Canvas.Width)||result.Height!=int(image.Canvas.Height)||result.DType!="float32"{t.Fatal("wrong inverse-field geometry")}}
  if !valid&&catalog.calls!=0{t.Fatal("invalid witness reached SQL")}
 })}
}

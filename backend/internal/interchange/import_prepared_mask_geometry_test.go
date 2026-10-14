package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)
func TestPreparedMaskGeometryRereadsParentAndCanonicalMaster(t *testing.T){
 for _,mode:=range []string{"valid","missing_witness","changed_parent","changed_master","foreign_role","cancelled"}{t.Run(mode,func(t *testing.T){
  _,masterRef,master:=importWorkingValidatorFixture(t);image,err:=review.ParseWorkingImage(master);if err!=nil{t.Fatal(err)}
  canvas,err:=canonical.Hash(image.Canvas);if err!=nil{t.Fatal(err)}
  binding:=v2input.SessionVersionBinding{AssetVersionID:uuid.New(),Source:masterRef.Ref,CanvasHash:canvas}
  pixels:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("pixels"))}
  mask:=review.MaskRef{Artifact:review.ArtifactRef{ID:pixels.ArtifactID,SHA256:pixels.SHA256},Canvas:image.Canvas,TransformChain:image.TransformChain,Selector:"manual",Confidence:1,Protected:true}
  raw,err:=canonical.Marshal(mask);if err!=nil{t.Fatal(err)}
  parent:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)};entry:=v2input.Entry{Contract:"MaskPixels",Ref:pixels}
  witnesses:=&ImportPreparedMaskWitnesses{}
  if mode!="missing_witness"{if err:=witnesses.remember(parent,raw,binding);err!=nil{t.Fatal(err)}}
  catalog:=&importCatalogStub{object:objectstore.Object{Key:"prepared-mask",VersionID:"v1",SHA256:parent.SHA256,Bytes:int64(len(raw)),ContentType:"application/json"}}
  objects:=&importReaderStub{body:append([]byte(nil),raw...)};masters:=&recipeSourceMasterReader{body:master}
  switch mode{case "changed_parent":objects.body[0]='!';case "changed_master":masters.second=[]byte(`{}`);case "foreign_role":entry.Contract="MaskArray"}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if mode=="cancelled"{cancel()}
  geometry,err:=(ImportPreparedMaskGeometry{Witnesses:witnesses,Catalog:catalog,Objects:objects,Masters:masters,Binding:binding}).ResolveExportArrayGeometry(ctx,entry)
  if (err==nil)!=(mode=="valid"){t.Fatal("unexpected mask geometry",err)}
  if mode=="valid"&&(geometry.CanvasSHA!=canvas||geometry.Width!=int(image.Canvas.Width)||geometry.Height!=int(image.Canvas.Height)||masters.calls!=4||objects.opens!=4){t.Fatal("mask parent/source rechecks missing")}
  if mode=="missing_witness"||mode=="foreign_role"||mode=="cancelled"{if objects.opens!=0||catalog.calls!=0{t.Fatal("invalid mask witness performed IO")}}
 })}
}
func TestPreparedMaskGeometryRepeatsCompleteParentSet(t *testing.T){
 for _,mode:=range []string{"valid","earlier_parent_changed","geometry_changed","registry_extended","callback_ownership","cancelled"}{t.Run(mode,func(t *testing.T){
  w:=&ImportPreparedMaskWitnesses{}
  entry:=v2input.Entry{Contract:"MaskPixels",Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte("pixels"))}}
  first:=preparedMaskWitness{parent:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA([]byte(`{}`))},body:[]byte(`{}`),child:entry}
  second:=first;second.parent.ArtifactID=uuid.New()
  if err:=w.rememberQualified(first);err!=nil{t.Fatal(err)}
  if err:=w.rememberQualified(second);err!=nil{t.Fatal(err)}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  calls:=0;changed:=false
  expected:=ExportArrayGeometry{Width:10,Height:10,CanvasSHA:rawSHA([]byte("canvas")),DType:"float32"}
  got,err:=resolvePreparedMaskWitnessSet(ctx,w,entry,func(_ context.Context,p preparedMaskWitness)(ExportArrayGeometry,error){
   calls++
   if mode=="callback_ownership"{if p.body[0]!='{'{t.Fatal("callback mutated repeat snapshot")};p.body[0]='!'}
   if mode=="earlier_parent_changed"{if p.parent==first.parent&&changed{return ExportArrayGeometry{},v2input.ErrInvalid};if p.parent==second.parent{changed=true}}
   if mode=="geometry_changed"&&calls==3{other:=expected;other.Width++;return other,nil}
   if mode=="registry_extended"&&calls==2{extra:=first;extra.parent.ArtifactID=uuid.New();if err:=w.rememberQualified(extra);err!=nil{t.Fatal(err)}}
   if mode=="cancelled"&&calls==2{cancel()}
   return expected,nil
  })
  valid:=mode=="valid"||mode=="callback_ownership"
  if (err==nil)!=valid{t.Fatal("unexpected complete parent set result",err)}
  if valid&&(calls!=4||got!=expected){t.Fatal("complete repeat missing")}
  if !valid&&got!=(ExportArrayGeometry{}){t.Fatal("failed qualification returned geometry")}
 })}
}

package v2learning

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "encoding/json"
 "os"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type regionMaskLoaderFixture struct { bodies map[v2input.Ref][]byte; calls int; cancel context.CancelFunc; mismatch bool }
func (m *regionMaskLoaderFixture) Load(ctx context.Context,who auth.Identity,start TrainingStart,candidate TrainingCandidate,ref v2input.Ref,image TrainingImage,protected bool)(TrainingMask,error){
 m.calls++
 mask,err:=review.ParseMaskRef(m.bodies[ref]);if err!=nil{return TrainingMask{},err}
 pixels:=v2input.Ref{ArtifactID:mask.Artifact.ID,SHA256:mask.Artifact.SHA256}
 if m.mismatch { pixels.ArtifactID=uuid.New() }
 if m.cancel!=nil { m.cancel() }
 return TrainingMask{Document:mask,Pixels:pixels},nil
}
func TestTrainingRegionsUseUniqueEventClosureMasks(t *testing.T){
 for _,mode:=range []string{"valid","shared_selector","missing","ambiguous","protected","confidence","duplicate_root","mismatch","cancelled","empty"}{t.Run(mode,func(t *testing.T){
  raw,err:=os.ReadFile("../review/testdata/working-images-python.json");if err!=nil{t.Fatal(err)}
  var images []json.RawMessage;if json.Unmarshal(raw,&images)!=nil||len(images)==0{t.Fatal("image fixture")}
  image,err:=review.ParseWorkingImage(images[0]);if err!=nil{t.Fatal(err)}
  bound:=TrainingImage{Document:image,PixelsCount:image.Canvas.Width*image.Canvas.Height}
  body,err:=canonical.Marshal(map[string]any{"artifact":v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)},"canvas":image.Canvas,"transform_chain":json.RawMessage(image.TransformChain),"object_id":nil,"selector":"manual","confidence":1.0,"model_lock_id":nil,"protected":false});if err!=nil{t.Fatal(err)}
  if mode=="protected"||mode=="confidence" {
   var fields map[string]any;if json.Unmarshal(body,&fields)!=nil{t.Fatal("mask")}
   if mode=="protected"{fields["protected"]=true}else{fields["confidence"]=.74}
   body,err=canonical.Marshal(fields);if err!=nil{t.Fatal(err)}
  }
  ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("b",64)}
  second:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("c",64)}
  bodies:=map[v2input.Ref][]byte{ref:body,second:body}
  refs:=maskRootListFixture{ref}
  switch mode{case "missing":refs=nil;case "ambiguous":refs=maskRootListFixture{ref,second};case "duplicate_root":refs=maskRootListFixture{ref,ref}}
  style:=[]byte(`{"extensions":{"region_adjustments":[{"region_id":"r1","selector":"manual"}]}}`)
  if mode=="shared_selector"{style=[]byte(`{"extensions":{"region_adjustments":[{"region_id":"r1","selector":"manual"},{"region_id":"r2","selector":"manual"}]}}`)}
  if mode=="empty"{style=[]byte(`{"extensions":{"region_adjustments":[]}}`)}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  loader:=&regionMaskLoaderFixture{bodies:bodies,mismatch:mode=="mismatch"}
  if mode=="cancelled"{loader.cancel=cancel}
  selector:=TrainingMaskSelector{Roots:refs,Reader:maskRootReaderFixture{bodies:bodies},Masks:loader}
  got,err:=selector.SelectRegions(ctx,auth.Identity{},TrainingStart{TenantID:uuid.New()},TrainingCandidate{},bound,style)
  valid:=mode=="valid"||mode=="shared_selector"||mode=="empty"
  if (err==nil)!=valid{t.Fatalf("mode=%s err=%v",mode,err)}
  if !valid&&got!=nil{t.Fatal("failed selection returned evidence")}
  if mode=="valid"||mode=="shared_selector"{
   count:=1;if mode=="shared_selector"{count=2}
   if len(got)!=count||got[0].RegionID!="r1"||got[0].Mask!=ref||loader.calls!=1{t.Fatal("region binding/load count")}
  }
  if mode=="empty"&&loader.calls!=0{t.Fatal("empty style read masks")}
 })}
}

func TestTrainingRegionSourceComposesPinnedStyleAndMaskProvenance(t *testing.T){
 for _,mode:=range []string{"valid","style_hash","mask_hash","provenance","missing_pixels","foreign_lineage"}{t.Run(mode,func(t *testing.T){
  raw,err:=os.ReadFile("../review/testdata/working-images-python.json");if err!=nil{t.Fatal(err)}
  var images []json.RawMessage;if json.Unmarshal(raw,&images)!=nil||len(images)==0{t.Fatal("image fixture")}
  image,err:=review.ParseWorkingImage(images[0]);if err!=nil{t.Fatal(err)}
  store:=trainingRecipeStore{objects:map[uuid.UUID]objectstore.Object{},bodies:map[string][]byte{}}
  publish:=func(key string,body []byte)v2input.Ref{
   ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:trainingRegionDigest(body)}
   store.objects[ref.ArtifactID]=objectstore.Object{Key:key,SHA256:ref.SHA256,Bytes:int64(len(body)),VersionID:"v1",ContentType:"application/json"}
   store.bodies[key]=body
   return ref
  }
  styleBody,err:=canonical.Bytes([]byte(`{"extensions":{"region_adjustments":[{"region_id":"region","selector":"manual"}]}}`));if err!=nil{t.Fatal(err)}
  style:=publish("style",styleBody)
  pixels:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}
  store.objects[pixels.ArtifactID]=objectstore.Object{Key:"pixels",SHA256:pixels.SHA256,Bytes:128,VersionID:"v1",ContentType:"application/octet-stream"}
  maskBody,err:=canonical.Marshal(map[string]any{"artifact":pixels,"canvas":image.Canvas,"transform_chain":json.RawMessage(image.TransformChain),"object_id":nil,"selector":"manual","confidence":1.0,"model_lock_id":nil,"protected":false});if err!=nil{t.Fatal(err)}
  mask:=publish("mask",maskBody)
  provenance:=&maskProvenanceFixture{reject:mode=="provenance"}
  selector:=TrainingMaskSelector{Roots:maskRootListFixture{mask},Reader:ScopedTrainingMaskRootReader{Catalog:store,Objects:store},Masks:TrainingMaskSource{Catalog:store,Objects:store,Provenance:provenance}}
  source:=TrainingRegionSource{Catalog:store,Objects:store,Selector:selector}
  candidate:=TrainingCandidate{EventID:uuid.New(),AssetVersionID:uuid.New(),SourceLineageSHA:image.Canvas.SourceSHA256}
  switch mode{
  case "style_hash":store.bodies["style"]=[]byte(strings.Repeat(" ",len(styleBody)))
  case "mask_hash":store.bodies["mask"]=[]byte(strings.Repeat(" ",len(maskBody)))
  case "missing_pixels":delete(store.objects,pixels.ArtifactID)
  case "foreign_lineage":candidate.SourceLineageSHA=strings.Repeat("f",64)
  }
  got,err:=source.LoadRegions(context.Background(),auth.Identity{},TrainingStart{TenantID:uuid.New(),ProjectID:uuid.New()},candidate,TrainingImage{Document:image,PixelsCount:image.Canvas.Width*image.Canvas.Height},style)
  if (err==nil)!=(mode=="valid"){t.Fatalf("mode=%s err=%v",mode,err)}
  if mode=="valid"&&(len(got)!=1||got[0].RegionID!="region"||got[0].Mask!=mask||provenance.calls!=1){t.Fatal("composed regional evidence missing")}
  if mode!="valid"&&got!=nil{t.Fatal("failed source returned regional evidence")}
 })}
}

func trainingRegionDigest(body []byte)string{sum:=sha256.Sum256(body);return hex.EncodeToString(sum[:])}

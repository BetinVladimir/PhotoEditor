package delivery

import (
 "context"
 "bytes"
 "io"
 "strings"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)
func TestWatermarkSourceEvidencePreflight(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=LoadWatermarkSourceEvidence(ctx,auth.Identity{},uuid.Nil,WatermarkSource{},nil,nil);err!=context.Canceled{t.Fatal(err)}
 if _,err:=LoadWatermarkSourceEvidence(context.Background(),auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},uuid.New(),WatermarkSource{Asset:uuid.New(),Version:uuid.New()},nil,nil);err==nil{t.Fatal("missing IO accepted")}
}

type watermarkEvidenceReader struct{body []byte;opens int}
func(r *watermarkEvidenceReader)Open(_ context.Context,_ objectstore.Object)(io.ReadCloser,error){r.opens++;return io.NopCloser(bytes.NewReader(r.body)),nil}
type watermarkEvidenceCatalog struct{source WatermarkSource;arrays map[string]objectstore.Object;calls int}
func(c *watermarkEvidenceCatalog)Lookup(_ context.Context,_ auth.Identity,_ uuid.UUID,node v2draft.LayerRef)(objectstore.Object,error){
 c.calls++
 if node.Contract=="WorkingImage"{if node.Ref!=c.source.Ref{panic("foreign manifest")};return c.source.Object,nil}
 if node.ParentRef!=c.source.Ref||node.ExpectedBinding==nil||node.ExpectedBinding.AssetVersionID!=c.source.Version||node.CanvasHash==""{panic("unbound array")}
 return c.arrays[node.Contract],nil
}
func TestWatermarkSourceEvidenceManifestAndArrays(t *testing.T){
 steps:=[]any{};hash,_:=canonical.Hash(steps);chain,_:=canonical.Marshal(map[string]any{"steps":steps,"hash":hash})
 pixel:=review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("a",64)};alpha:=review.ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("b",64)}
 image:=review.WorkingImage{Pixels:pixel,Alpha:&alpha,Canvas:review.Canvas{Width:100,Height:50,Orientation:1,SpaceID:"watermark:test",SourceSHA256:strings.Repeat("c",64)},Color:review.ColorSpace{ProfileID:"work_rec2020_gamma24_v1",ICCSHA256:strings.Repeat("d",64),Encoding:"gamma24"},DType:"float32",TransformChain:chain}
 raw,_:=canonical.Marshal(image)
 source:=WatermarkSource{Asset:uuid.New(),Version:uuid.New(),Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:digest(raw)},Object:objectstore.Object{Key:"manifest",VersionID:"v1",Bytes:int64(len(raw)),SHA256:digest(raw),ContentType:"application/json"}}
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}
 for _,mode:=range []string{"exact","uint16_alpha","wrong_mime","undersized","unversioned","tampered_manifest"}{t.Run(mode,func(t *testing.T){
  catalog:=&watermarkEvidenceCatalog{source:source,arrays:map[string]objectstore.Object{
   "WorkingPixels":{Key:"rgb",VersionID:"v1",SHA256:pixel.SHA256,Bytes:100*50*3*4+128,ContentType:"application/octet-stream"},
   "WorkingAlpha":{Key:"alpha",VersionID:"v1",SHA256:alpha.SHA256,Bytes:100*50*4+128,ContentType:"application/x-npy"},
  }}
  current:=catalog.arrays["WorkingAlpha"]
  switch mode{case "uint16_alpha":current.Bytes=100*50*2+128;case "wrong_mime":current.ContentType="image/png";case "undersized":current.Bytes=1;case "unversioned":current.VersionID="null"}
  catalog.arrays["WorkingAlpha"]=current
  reader:=&watermarkEvidenceReader{body:append([]byte(nil),raw...)};if mode=="tampered_manifest"{reader.body[0]='!'}
  evidence,err:=LoadWatermarkSourceEvidence(context.Background(),who,uuid.New(),source,reader,catalog)
  valid:=mode=="exact"||mode=="uint16_alpha"
  if (err==nil)!=valid{t.Fatal(mode,err)}
  if valid&&(len(evidence.Arrays)!=2||reader.opens!=1||catalog.calls!=3){t.Fatal("incomplete evidence",reader.opens,catalog.calls)}
  if mode=="tampered_manifest"&&catalog.calls!=1{t.Fatal("array lookup after invalid manifest")}
 })}
}

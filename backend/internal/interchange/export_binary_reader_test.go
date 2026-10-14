package interchange
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type exportBinaryQualifierStub struct{calls int;fail bool}
func(s *exportBinaryQualifierStub)QualifyExportBinary(context.Context,v2input.Entry,objectstore.Object)error{s.calls++;if s.fail{return v2input.ErrInvalid};return nil}
func TestExportBinaryReaderRequiresPhysicalQualificationAndCurrentPin(t *testing.T){
 for _,mode:=range []string{"encoded","array","npy","npy_mask","hash","size","media","version","qualification","missing_qualifier","contract"}{t.Run(mode,func(t *testing.T){
  body:=[]byte("binary image");entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)},Contract:"EncodedImage"}
  catalog:=&importCatalogStub{object:objectstore.Object{Key:"key",VersionID:"version",Bytes:int64(len(body)),SHA256:entry.Ref.SHA256,ContentType:"image/tiff"}}
  objects:=&importReaderStub{body:body};qualifier:=&exportBinaryQualifierStub{}
  r:=ExportBinaryNodeResolver{Catalog:exportReaderCatalog{catalog},Objects:objects,Qualifier:qualifier,MaxBytes:1024}
  switch mode{case "npy":entry.Contract="DenseWarpField";catalog.object.ContentType="application/x-npy";case "npy_mask":entry.Contract="MaskPixels";catalog.object.ContentType="application/x-npy";case "array":entry.Contract="MaskArray";catalog.object.ContentType="application/octet-stream";case "hash":objects.body=[]byte("substitution");case "size":catalog.object.Bytes++;case "media":catalog.object.ContentType="application/json";case "version":catalog.change=true;case "qualification":qualifier.fail=true;case "missing_qualifier":r.Qualifier=nil;case "contract":entry.Contract="Artifact"}
  node,err:=r.ResolveExportNode(context.Background(),entry)
  if mode=="encoded"||mode=="array"||mode=="npy"||mode=="npy_mask"{if err!=nil||node.Entry!=entry||node.Body!=nil||qualifier.calls!=1||catalog.calls!=2{t.Fatal("invalid binary qualification",err)}}else if err==nil{t.Fatal("invalid binary accepted")}
  if mode=="media"||mode=="missing_qualifier"||mode=="contract"{if objects.opens!=0||qualifier.calls!=0{t.Fatal("invalid input reached IO")}}
  if mode=="hash"||mode=="size"{if qualifier.calls!=0{t.Fatal("bad bytes reached qualifier")}}
 })}
}

package interchange
import (
 "bytes"
 "context"
 "io"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type exportBundleCatalogStub struct{object objectstore.Object;calls int;change bool}
func(s *exportBundleCatalogStub)LookupExportOutput(_ context.Context,e v2input.Entry)(objectstore.Object,error){
 if e.Contract!="InterchangeArchive"{return objectstore.Object{},v2input.ErrInvalid};s.calls++;o:=s.object;if s.change&&s.calls>1{o.VersionID="changed"};return o,nil
}
type exportBundleObjectsStub struct{body []byte;opens,closes int;empty bool}
type exportBundleStream struct{io.Reader;owner *exportBundleObjectsStub}
func(s *exportBundleStream)Close()error{s.owner.closes++;return nil}
type exportEmptyStream struct{}
func(exportEmptyStream)Read([]byte)(int,error){return 0,nil}
func(s *exportBundleObjectsStub)Open(context.Context,objectstore.Object)(io.ReadCloser,error){
 s.opens++;var reader io.Reader=bytes.NewReader(s.body);if s.empty{reader=exportEmptyStream{}}
 return &exportBundleStream{reader,s},nil
}
func TestExportBundleReaderChecksPhysicalBytesAndRechecksVersion(t *testing.T){
 intent,compiled,snapshot,raw:=exportFreezeFixture(t);frozen,err:=FreezeExportDocuments(context.Background(),intent,compiled,snapshot,raw);if err!=nil{t.Fatal(err)}
 for _,kind:=range []string{"valid","hash","short","long","version","media","quota","no_progress"}{t.Run(kind,func(t *testing.T){
  body:=[]byte("physical-byte-proof-is-not-ZIP-semantic-proof")
  ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(body)}
  catalog:=&exportBundleCatalogStub{object:objectstore.Object{Key:"bundle",VersionID:"v1",ContentType:"application/zip",SHA256:ref.SHA256,Bytes:int64(len(body))}}
  objects:=&exportBundleObjectsStub{body:append([]byte(nil),body...)}
  switch kind{case "hash":objects.body[0]='x';case "short":objects.body=objects.body[:2];case "long":objects.body=append(objects.body,'x');case "version":catalog.change=true;case "media":catalog.object.ContentType="application/json";case "quota":catalog.object.Bytes=2<<20;case "no_progress":objects.empty=true}
  receipt,err:=(ExportBundleReader{Catalog:catalog,Objects:objects}).Read(context.Background(),intent,frozen,ref)
  if kind=="valid"{if err!=nil||receipt.Ref()!=ref||receipt.Bytes()!=int64(len(body))||catalog.calls!=2{t.Fatal("valid byte proof refused",err)}}else if err==nil{t.Fatal("invalid bytes accepted")}
  if objects.opens!=objects.closes{t.Fatal("stream leaked")}
 })}
}

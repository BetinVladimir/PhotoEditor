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
type importCatalogStub struct{object objectstore.Object;calls int;change bool}
func(s *importCatalogStub)LookupImportDocument(context.Context,v2input.Entry)(objectstore.Object,error){s.calls++;o:=s.object;if s.change&&s.calls>1{o.VersionID="new-version"};return o,nil}
type importReaderStub struct{body []byte;opens int}
func(s *importReaderStub)Open(context.Context,objectstore.Object)(io.ReadCloser,error){s.opens++;return io.NopCloser(bytes.NewReader(s.body)),nil}
type importValidatorStub struct{calls int}
func(s *importValidatorStub)ImportDocumentChildren(_ context.Context,_ v2input.Entry,raw []byte)([]v2input.Entry,error){s.calls++;raw[0]='x';return nil,nil}
func importReaderFixture()(v2input.Entry,*importCatalogStub,*importReaderStub,*importValidatorStub){
 raw:=[]byte(`{"schema_version":1}`);entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"InterchangePlan"}
 return entry,&importCatalogStub{object:objectstore.Object{Key:"key",VersionID:"version-1",ContentType:"application/json",Bytes:int64(len(raw)),SHA256:entry.Ref.SHA256}},&importReaderStub{body:raw},&importValidatorStub{}
}
func TestImportDocumentReaderChecksCurrentObjectAndOwnsValidationBytes(t *testing.T){
 entry,catalog,objects,validator:=importReaderFixture();resolver:=ImportJSONNodeResolver{Catalog:catalog,Objects:objects,Validator:validator}
 node,err:=resolver.ResolveImportNode(context.Background(),entry)
 if err!=nil||catalog.calls!=2||objects.opens!=1||validator.calls!=1||rawSHA(node.Body)!=entry.Ref.SHA256{t.Fatal("read",err)}
}
func TestImportDocumentReaderRejectsPhysicalSubstitutionBeforeTypedValidation(t *testing.T){
 entry,catalog,objects,validator:=importReaderFixture();objects.body=[]byte(`{"schema_version":2}`)
 if _,err:=(ImportJSONNodeResolver{Catalog:catalog,Objects:objects,Validator:validator}).ResolveImportNode(context.Background(),entry);err==nil||validator.calls!=0{t.Fatal("substitution validated")}
}
func TestImportDocumentReaderRejectsVersionChangeAfterRead(t *testing.T){
 entry,catalog,objects,validator:=importReaderFixture();catalog.change=true
 if _,err:=(ImportJSONNodeResolver{Catalog:catalog,Objects:objects,Validator:validator}).ResolveImportNode(context.Background(),entry);err==nil||catalog.calls!=2{t.Fatal("changed object accepted")}
}
func TestImportDocumentReaderRejectsBinaryBeforeObjectIO(t *testing.T){
 entry,catalog,objects,validator:=importReaderFixture();catalog.object.ContentType="application/octet-stream"
 if _,err:=(ImportJSONNodeResolver{Catalog:catalog,Objects:objects,Validator:validator}).ResolveImportNode(context.Background(),entry);err==nil||objects.opens!=0{t.Fatal("binary JSON accepted")}
}

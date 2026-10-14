package interchange

import (
 "context"
 "errors"
 "io"
 "testing"

 "photoeditor/backend/internal/objectstore"
)

func TestImportMasterReaderVerifiesTypedBytesAndCurrentVersion(t *testing.T){
 _,entry,body:=importWorkingValidatorFixture(t)
 catalog:=&importCatalogStub{object:objectstore.Object{Key:"master",VersionID:"v1",ContentType:"application/json",Bytes:int64(len(body)),SHA256:entry.Ref.SHA256}}
 objects:=&importReaderStub{body:body};reader:=ImportObjectMasterReader{Catalog:catalog,Objects:objects}
 result,err:=reader.ReadImportMaster(context.Background(),entry.Ref,1<<20)
 if err!=nil||string(result)!=string(body)||catalog.calls!=2||objects.opens!=1{t.Fatal("master read",err)}
 result[0]='x';if body[0]!='{'{t.Fatal("reader aliases object bytes")}
}

func TestImportMasterReaderRejectsBadPhysicalEvidence(t *testing.T){
 for _,change:=range []string{"hash","short","extra","version","mime","budget","typed"}{t.Run(change,func(t *testing.T){
  _,entry,body:=importWorkingValidatorFixture(t)
  catalog:=&importCatalogStub{object:objectstore.Object{Key:"master",VersionID:"v1",ContentType:"application/json",Bytes:int64(len(body)),SHA256:entry.Ref.SHA256}}
  objects:=&importReaderStub{body:body};limit:=int64(1<<20)
  switch change{
  case "hash":objects.body=append([]byte(nil),body...);objects.body[0]='x'
  case "short":objects.body=body[:2]
  case "extra":objects.body=append(append([]byte(nil),body...),byte('x'))
  case "version":catalog.change=true
  case "mime":catalog.object.ContentType="application/octet-stream"
  case "budget":limit=2
  case "typed":objects.body=[]byte(`{}`);entry.Ref.SHA256=rawSHA(objects.body);catalog.object.SHA256=entry.Ref.SHA256;catalog.object.Bytes=2
  }
  if _,err:=(ImportObjectMasterReader{Catalog:catalog,Objects:objects}).ReadImportMaster(context.Background(),entry.Ref,limit);err==nil{t.Fatal("invalid master accepted")}
  if (change=="budget"||change=="mime")&&objects.opens!=0{t.Fatal("invalid metadata reached object IO")}
 })}
}

func TestImportMasterReaderCancellationPreventsIO(t *testing.T){
 _,entry,_:=importWorkingValidatorFixture(t);catalog:=&importCatalogStub{};objects:=&importReaderStub{}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=(ImportObjectMasterReader{Catalog:catalog,Objects:objects}).ReadImportMaster(ctx,entry.Ref,1<<20);!errors.Is(err,context.Canceled)||catalog.calls!=0||objects.opens!=0{t.Fatal("cancelled read",err)}
}

// A storage adapter may return a partial resource together with an error.
// Both readers own that resource and must close it without consuming bytes.
type importInterruptedStream struct{cancel context.CancelFunc;reads,closes int}
func(s *importInterruptedStream)Read(p []byte)(int,error){
 s.reads++;if s.cancel!=nil{s.cancel()};p[0]='{';return 1,nil
}
func(s *importInterruptedStream)Close()error{s.closes++;return nil}
type importInterruptedObjects struct{stream *importInterruptedStream;err error}
func(s importInterruptedObjects)Open(context.Context,objectstore.Object)(io.ReadCloser,error){return s.stream,s.err}

func TestImportReadersClosePartialOpenAndStopCancelledReads(t *testing.T){
 for _,master:=range []bool{false,true}{for _,openFailure:=range []bool{false,true}{
  _,entry,body:=importWorkingValidatorFixture(t)
  ctx,cancel:=context.WithCancel(context.Background())
  stream:=&importInterruptedStream{cancel:cancel}
  storageErr:=errors.New("storage open interrupted")
  objects:=importInterruptedObjects{stream:stream}
  if openFailure{objects.err=storageErr}
  catalog:=&importCatalogStub{object:objectstore.Object{Key:"master",VersionID:"v1",ContentType:"application/json",Bytes:int64(len(body)),SHA256:entry.Ref.SHA256}}
  var err error
  if master{_,err=(ImportObjectMasterReader{Catalog:catalog,Objects:objects}).ReadImportMaster(ctx,entry.Ref,1<<20)}else{
   _,err=(ImportJSONNodeResolver{Catalog:catalog,Objects:objects,Validator:&importValidatorStub{}}).ResolveImportNode(ctx,entry)
  }
  cancel()
  if openFailure{if !errors.Is(err,storageErr)||stream.reads!=0{t.Fatal("partial open consumed",err,stream.reads)}}else{
   if !errors.Is(err,context.Canceled)||stream.reads!=1{t.Fatal("read continued after cancellation",err,stream.reads)}
  }
  if stream.closes!=1||catalog.calls!=1{t.Fatal("resource leak or authority lookup after failed IO",stream.closes,catalog.calls)}
 }}
}

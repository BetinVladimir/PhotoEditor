package interchange

import (
 "context"
 "errors"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

func TestPreparedReaderPinsCanonicalBytesAndLiveCatalog(t *testing.T){
 for _,change:=range []string{"none","version","hash","size","extra","noncanonical","array","media","limit","binary","cancelled"}{t.Run(change,func(t *testing.T){
  raw:=[]byte(`{"schema_version":1}`)
  if change=="noncanonical"{raw=[]byte(`{ "schema_version": 1 }`)}
  if change=="array"{raw=[]byte(`[]`)}
  ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)}
  object:=objectstore.Object{Key:"prepared",VersionID:"v1",SHA256:ref.SHA256,Bytes:int64(len(raw)),ContentType:"application/json"}
  if change=="hash"{ref.SHA256=rawSHA([]byte("other"));object.SHA256=ref.SHA256}
  if change=="size"{object.Bytes++}
  if change=="media"{object.ContentType="application/octet-stream"}
  catalog:=&importCatalogStub{object:object,change:change=="version"}
  body:=raw;if change=="extra"{body=append(append([]byte(nil),raw...),byte('x'))}
  objects:=&importReaderStub{body:body}
  reader:=BoundedPreparedImportReader{Catalog:catalog,Objects:objects}
  entry:=v2input.Entry{Ref:ref,Contract:"StyleV2"};if change=="binary"{entry.Contract="MaskPixels"}
  limit:=int64(8<<20);if change=="limit"{limit=2}
  ctx,cancel:=context.WithCancel(context.Background());defer cancel();if change=="cancelled"{cancel()}
  got,err:=reader.ReadPreparedImportDocument(ctx,entry,limit)
  if change=="none"{if err!=nil||string(got)!=string(raw)||catalog.calls!=2||objects.opens!=1{t.Fatal("immutable read",err)};return}
  if err==nil||got!=nil{t.Fatal("invalid bytes/authority returned",err)}
  if change=="cancelled"&&!errors.Is(err,context.Canceled){t.Fatal(err)}
  if (change=="media"||change=="limit"||change=="binary"||change=="cancelled")&&objects.opens!=0{t.Fatal("rejected request performed object IO")}
 })}
}

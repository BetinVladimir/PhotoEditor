package interchange

import (
 "context"
 "errors"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

func TestPreparedCatalogExactObjectAuthority(t *testing.T){
 scope:=ImportPreparedScope{Tenant:uuid.New(),Run:uuid.New()}
 ref:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}
 key,err:=objectstore.DocumentKey(scope.Tenant,scope.Run,ref.ArtifactID);if err!=nil{t.Fatal(err)}
 good:=objectstore.Object{Key:key,VersionID:"pinned",SHA256:ref.SHA256,Bytes:32,ContentType:"application/json"}
 cases:=[]struct{name string;change func(*objectstore.Object,*string)}{
  {"foreign bucket",func(_ *objectstore.Object,b *string){*b="another-bucket"}},
  {"foreign key",func(o *objectstore.Object,_ *string){o.Key+="/other"}},
  {"missing version",func(o *objectstore.Object,_ *string){o.VersionID=""}},
  {"null version",func(o *objectstore.Object,_ *string){o.VersionID="null"}},
  {"hash substitution",func(o *objectstore.Object,_ *string){o.SHA256=strings.Repeat("b",64)}},
  {"wrong media",func(o *objectstore.Object,_ *string){o.ContentType="application/octet-stream"}},
  {"empty JSON",func(o *objectstore.Object,_ *string){o.Bytes=1}},
  {"budget",func(o *objectstore.Object,_ *string){o.Bytes=65}},
 }
 scan:=func(object objectstore.Object,bucket string)(objectstore.Object,error){
  row:=ledgerRow(func(d ...any)error{*d[0].(*string)=bucket;*d[1].(*string)=object.Key;*d[2].(*string)=object.VersionID;*d[3].(*string)=object.SHA256;*d[4].(*int64)=object.Bytes;*d[5].(*string)=object.ContentType;return nil})
  return scanPreparedImportObject(row,scope,ref,"photo-bucket","application/json",64)
 }
 if got,err:=scan(good,"photo-bucket");err!=nil||got!=good{t.Fatal("exact pinned object rejected",err)}
 for _,c:=range cases{t.Run(c.name,func(t *testing.T){object:=good;bucket:="photo-bucket";c.change(&object,&bucket);got,err:=scan(object,bucket);if !errors.Is(err,objectstore.ErrIntegrity)||got!=(objectstore.Object{}){t.Fatal("changed authority returned",got,err)}})}
 failure:=errors.New("catalog unavailable")
 if _,err:=scanPreparedImportObject(ledgerRow(func(...any)error{return failure}),scope,ref,"photo-bucket","application/json",64);!errors.Is(err,failure){t.Fatal("database failure lost",err)}
}

func TestPreparedCatalogCancelledAndUninstalled(t *testing.T){
 catalog:=SQLPreparedImportCatalog{}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=catalog.LookupImportDocument(ctx,v2input.Entry{});!errors.Is(err,context.Canceled){t.Fatal(err)}
 if _,err:=catalog.LookupImportDocument(context.Background(),v2input.Entry{});!errors.Is(err,v2input.ErrInvalid){t.Fatal("uninstalled catalog accepted",err)}
}

package delivery

import (
 "context"
 "strings"
 "testing"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)
type metadataSourceReadDB struct{body,pin string;calls int;err error}
func(d *metadataSourceReadDB)QueryRow(_ context.Context,q string,args ...any)pgx.Row{
 if !strings.Contains(q,"photo.read_v30_metadata_source(")||len(args)!=6{panic("wrong source read boundary")}
 d.calls++
 return metadataStorageRow{func(dest []any)error{
  if d.err!=nil{return d.err}
  *dest[0].(*string)=d.body;*dest[1].(*string)=d.pin;return nil
 }}
}
func TestMetadataPublishedSourceReadRestoresExternalScope(t *testing.T){
 repository,_:=sourceStorageFixture(t)
 scope:=MetadataSourceReadScope{Tenant:repository.scope.Tenant,ParentRun:repository.run,Binding:repository.scope.Binding,Metadata:repository.plan.Ref(),Policy:"copyright"}
 db:=&metadataSourceReadDB{body:string(repository.plan.JCS()),pin:repository.plan.SHA256()};store:=MetadataSourceStore{db}
 result,err:=store.Load(context.Background(),scope)
 if err!=nil||result.Ref()!=scope.Metadata||result.SHA256()!=db.pin{t.Fatal("restore",err)}
 for _,mode:=range []string{"tenant","canvas","policy","approval","hash","body","missing"}{
  current:=scope;db.body=string(repository.plan.JCS());db.pin=repository.plan.SHA256();db.err=nil
  switch mode{
  case "tenant":current.Tenant=repository.scope.Actor
  case "canvas":current.Binding.CanvasHash=strings.Repeat("e",64)
  case "policy":current.Policy="approved_exif"
  case "approval":current.ApprovedFields=[]string{"gps"}
  case "hash":db.pin=strings.Repeat("e",64)
  case "body":db.body="{}";db.pin=digest([]byte(db.body))
  case "missing":db.err=pgx.ErrNoRows
  }
  if _,err:=store.Load(context.Background(),current);err==nil{t.Fatal(mode)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel();calls:=db.calls
 if _,err=store.Load(ctx,scope);err!=context.Canceled||db.calls!=calls{t.Fatal("cancel",err)}
 scope.ApprovedFields=[]string{"private_serial"}
 if _,err=store.Load(context.Background(),scope);err!=v2input.ErrInvalid||db.calls!=calls{t.Fatal("invalid approval reached DB",err)}
}

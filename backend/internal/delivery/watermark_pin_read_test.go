package delivery

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)
type watermarkReadDB struct{raw,pin string;err error;calls int}
func(d *watermarkReadDB)QueryRow(_ context.Context,q string,args ...any)pgx.Row{
 if q!=watermarkPinReadQuery||len(args)!=6{panic("scheduler pin ABI")};d.calls++
 return metadataStorageRow{func(dest []any)error{if d.err!=nil{return d.err};*dest[0].(*string)=d.raw;*dest[1].(*string)=d.pin;return nil}}
}
func TestSchedulerWatermarkPinRestoresExternalScope(t *testing.T){
 scope,config,e:=watermarkPinFixture(t);pin,err:=CompileWatermarkSourcePin(scope,config,e);if err!=nil{t.Fatal(err)}
 db:=&watermarkReadDB{raw:string(pin.JCS()),pin:pin.SHA256()};store:=WatermarkPinStore{DB:db};expansion,token:=uuid.New(),uuid.New()
 restored,err:=store.Read(context.Background(),scope,expansion,token,config);if err!=nil||restored.SHA256()!=pin.SHA256(){t.Fatal(err)}
 foreign:=scope;foreign.Actor=uuid.New()
 if _,err=store.Read(context.Background(),foreign,expansion,token,config);err==nil{t.Fatal("foreign actor pin restored")}
 db.err=pgx.ErrNoRows
 if _,err=store.Read(context.Background(),scope,expansion,token,config);err!=v2input.ErrExpired{t.Fatal("expired fence",err)}
 before:=db.calls
 if _,err=store.Read(context.Background(),scope,expansion,uuid.Nil,config);err==nil||db.calls!=before{t.Fatal("unfenced read")}
}

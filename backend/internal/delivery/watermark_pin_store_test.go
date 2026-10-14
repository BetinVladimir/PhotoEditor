package delivery

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)
type watermarkPinDB struct{calls int;args []any;accepted bool}
func(d *watermarkPinDB)QueryRow(_ context.Context,q string,args ...any)pgx.Row{
 if q!=`SELECT photo.pin_v30_watermark_source($1,$2,$3,$4,$5,$6,$7,$8)`||len(args)!=8{panic("pin ABI")};d.calls++;d.args=append([]any(nil),args...)
 return metadataStorageRow{func(dest []any)error{*dest[0].(*bool)=d.accepted;return nil}}
}
func TestWatermarkPinStoreScopeAndCurrentSession(t *testing.T){
 intent,_,_,_:=createDocumentsFixture(t);scope,config,evidence:=watermarkPinFixture(t)
 scope.Tenant=intent.tenant;scope.Project=intent.request.ProjectID;scope.Actor=intent.actor;scope.Parent=intent.identity.Run;scope.Preset=intent.request.Policy.RequiredPresets[0]
 pin,err:=CompileWatermarkSourcePin(scope,config,evidence);if err!=nil{t.Fatal(err)}
 db:=&watermarkPinDB{accepted:true};current:=uuid.New()
 if err=intent.PinWatermarkSource(context.Background(),db,current,pin);err!=nil{t.Fatal(err)}
 if db.calls!=1||db.args[3]!=current||db.args[4]!=intent.key||db.args[5]!=intent.requestSHA||db.args[7]!=pin.SHA256(){t.Fatal("lost authority scope")}
 db.accepted=false
 if err=intent.PinWatermarkSource(context.Background(),db,current,pin);err!=v2input.ErrExpired{t.Fatal("refusal",err)}
 for _,mode:=range []string{"tenant","project","actor","parent","preset"}{
  foreign:=scope;switch mode{case "tenant":foreign.Tenant=uuid.New();case "project":foreign.Project=uuid.New();case "actor":foreign.Actor=uuid.New();case "parent":foreign.Parent=uuid.New();case "preset":foreign.Preset=uuid.New()}
  changed,err:=CompileWatermarkSourcePin(foreign,config,evidence);if err!=nil{t.Fatal(err)};before:=db.calls
  if err=intent.PinWatermarkSource(context.Background(),db,current,changed);err==nil||db.calls!=before{t.Fatal("foreign pin reached SQL",mode)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err=intent.PinWatermarkSource(ctx,db,current,pin);err!=context.Canceled{t.Fatal(err)}
 if err=intent.PinWatermarkSource(context.Background(),db,current,FrozenWatermarkSource{});err==nil{t.Fatal("zero pin")}
}

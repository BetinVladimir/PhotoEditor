package delivery

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)
type watermarkRetryDB struct{calls int;outcome string;raw,pin *string;err error;args []any}
func(d *watermarkRetryDB)QueryRow(_ context.Context,q string,args ...any)pgx.Row{
 if q!=watermarkPinRetryQuery||len(args)!=9{panic("watermark retry ABI")};d.calls++;d.args=append([]any(nil),args...)
 return metadataStorageRow{func(dest []any)error{if d.err!=nil{return d.err};*dest[0].(*string)=d.outcome;*dest[1].(**string)=d.raw;*dest[2].(**string)=d.pin;return nil}}
}
func TestWatermarkRetryReadAuthorityAndOutcomes(t *testing.T){
 intent,_,_,_:=createDocumentsFixture(t);scope,config,e:=watermarkPinFixture(t)
 scope.Tenant=intent.tenant;scope.Project=intent.request.ProjectID;scope.Actor=intent.actor;scope.Parent=intent.identity.Run;scope.Preset=intent.request.Policy.RequiredPresets[0]
 frozen,err:=CompileWatermarkSourcePin(scope,config,e);if err!=nil{t.Fatal(err)}
 raw,pin:=string(frozen.JCS()),frozen.SHA256();session:=uuid.New()
 db:=&watermarkRetryDB{outcome:"saved",raw:&raw,pin:&pin}
 restored,found,err:=intent.ReadWatermarkPinRetry(context.Background(),db,session,scope,config)
 if err!=nil||!found||restored.SHA256()!=pin||db.args[4]!=session||db.args[8]!=intent.requestSHA{t.Fatal("saved authority",found,err)}
 db=&watermarkRetryDB{outcome:"absent"}
 if _,found,err=intent.ReadWatermarkPinRetry(context.Background(),db,session,scope,config);err!=nil||found{t.Fatal("absence",found,err)}
 db=&watermarkRetryDB{err:pgx.ErrNoRows}
 if _,_,err=intent.ReadWatermarkPinRetry(context.Background(),db,session,scope,config);err!=v2input.ErrExpired{t.Fatal("expired authority treated as absence",err)}
 db=&watermarkRetryDB{outcome:"absent",raw:&raw}
 if _,_,err=intent.ReadWatermarkPinRetry(context.Background(),db,session,scope,config);err==nil{t.Fatal("malformed absence")}
 foreign:=scope;foreign.Parent=uuid.New();db=&watermarkRetryDB{}
 if _,_,err=intent.ReadWatermarkPinRetry(context.Background(),db,session,foreign,config);err==nil||db.calls!=0{t.Fatal("foreign scope queried")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,_,err=intent.ReadWatermarkPinRetry(ctx,db,session,scope,config);err!=context.Canceled||db.calls!=0{t.Fatal("cancelled query",err)}
}

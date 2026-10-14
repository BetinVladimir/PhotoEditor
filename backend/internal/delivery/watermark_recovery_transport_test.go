package delivery
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
)
func TestWatermarkSelectionRecoveryRejectsMissingStaleForeign(t *testing.T){
 intent,session,_,_:=createDocumentsFixture(t);who:=auth.Identity{UserID:intent.actor,SessionID:session}
 missing:=&recoveryTx{allowed:true,missing:true}
 result,err:=intent.ReadWatermarkSelectionPending(context.Background(),missing,who,intent.tenant);if err!=nil||result.Found||missing.calls!=2{t.Fatal("missing recovery",err)}
 denied:=&recoveryTx{allowed:false};if _,err:=intent.ReadWatermarkSelectionPending(context.Background(),denied,who,intent.tenant);err==nil||denied.calls!=1{t.Fatal("authorization refusal")}
 conflict:=&recoveryTx{allowed:true,live:true,hash:"foreign"};if _,err:=intent.ReadWatermarkSelectionPending(context.Background(),conflict,who,intent.tenant);err==nil{t.Fatal("request mismatch")}
 expired:=&recoveryTx{allowed:true,hash:intent.RequestSHA()};if _,err:=intent.ReadWatermarkSelectionPending(context.Background(),expired,who,intent.tenant);err==nil{t.Fatal("expired pending")}
 foreign:=&recoveryTx{};if _,err:=intent.ReadWatermarkSelectionPending(context.Background(),foreign,who,uuid.New());err==nil||foreign.calls!=0{t.Fatal("foreign tenant queried")}
 ctx,cancel:=context.WithCancel(context.Background());cancel();if _,err:=intent.ReadWatermarkSelectionPending(ctx,foreign,who,intent.tenant);err!=context.Canceled||foreign.calls!=0{t.Fatal("cancel ignored")}
 if _,err:=(Service{}).RecoverWatermarkSelection(context.Background(),who,intent.tenant,intent.Request(),intent.key);err==nil{t.Fatal("nil DB")}
 if _,err:=(Service{}).StageWatermarkCreateTransport(ctx,who,intent.tenant,intent.Request(),intent.key,"photos",nil,nil,PrepareResourceLimits{});err!=context.Canceled{t.Fatal("transport cancel",err)}
 if _,err:=(Service{}).StageWatermarkCreateTransport(context.Background(),who,intent.tenant,intent.Request(),intent.key,"photos",nil,nil,PrepareResourceLimits{});err==nil{t.Fatal("missing publisher")}
}

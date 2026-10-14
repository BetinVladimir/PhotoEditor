package delivery
import (
 "context"
 "testing"
 "photoeditor/backend/internal/auth"
)
func TestWatermarkPreparePipelineRejectsUnavailableAndCancelled(t *testing.T){
 intent,session,_,_:=createDocumentsFixture(t);who:=auth.Identity{UserID:intent.actor,SessionID:session}
 if _,err:=(Service{}).PreparePinnedWatermarkDelivery(context.Background(),who,intent.tenant,intent.Request(),intent.key,CreateDeliveryDependencies{});err==nil{t.Fatal("unavailable dependencies")}
 if err:=(Service{}).AdmitPreparedWatermarkCreate(context.Background(),who,intent.tenant,intent.Request(),intent.key,nil);err==nil{t.Fatal("unavailable API DB")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=(Service{}).PreparePinnedWatermarkDelivery(ctx,who,intent.tenant,intent.Request(),intent.key,CreateDeliveryDependencies{});err!=context.Canceled{t.Fatal("cancelled pipeline",err)}
 if err:=(Service{}).AdmitPreparedWatermarkCreate(ctx,who,intent.tenant,intent.Request(),intent.key,nil);err!=context.Canceled{t.Fatal("cancelled admission",err)}
}

func TestWatermarkPendingPreparationRefusesMissingAuthority(t *testing.T){
 intent,session,_,_:=createDocumentsFixture(t);who:=auth.Identity{UserID:intent.actor,SessionID:session}
 if _,err:=(Service{}).PrepareWatermarkCreatePending(context.Background(),who,intent.tenant,intent.Request(),intent.key,nil);err==nil{t.Fatal("missing authority accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=(Service{}).PinWatermarkRecipeSelection(ctx,who,intent.tenant,intent.Request(),intent.key);err!=context.Canceled{t.Fatal("cancel ignored",err)}
}

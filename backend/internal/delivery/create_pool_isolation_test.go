package delivery

import (
 "context"
 "errors"
 "testing"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

type uncalledCreateObjects struct{CreatePreparationObjects}
type uncalledCreateDocuments struct{v2input.DocumentPublisher}
type uncalledCreateFreezer struct{CreateRecipeFreezer}

func TestDeliveryPreparationRequiresSeparateAPIAndSchedulerPools(t *testing.T){
 api,scheduler:=&pgxpool.Pool{},&pgxpool.Pool{}
 if !deliveryPoolsIsolated(api,scheduler){t.Fatal("separate pools rejected")}
 if deliveryPoolsIsolated(api,api)||deliveryPoolsIsolated(nil,scheduler)||deliveryPoolsIsolated(api,nil){t.Fatal("shared or missing privileged pool accepted")}
}

func TestDeliveryPreparersRefuseSharedPoolBeforeAnyDependentIO(t *testing.T){
 shared:=&pgxpool.Pool{};service:=Service{DB:shared}
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tenant:=uuid.New()
 request:=createRequestFixture();deps:=CreateDeliveryDependencies{
  Scheduler:shared,Bucket:"photos",Objects:&uncalledCreateObjects{},Documents:&uncalledCreateDocuments{},Freezer:&uncalledCreateFreezer{},
  Limits:PrepareResourceLimits{RAMBytes:1<<30,ScratchBytes:1<<30,OutputBytes:8<<20},
 }
 calls:=[]struct{name string;run func()(PreparedCreateDelivery,error)}{
  {"ordinary",func()(PreparedCreateDelivery,error){return service.PrepareDelivery(context.Background(),who,tenant,request,"pool-isolation",deps)}},
  {"selected",func()(PreparedCreateDelivery,error){return service.PrepareSelectedDelivery(context.Background(),who,tenant,request,"pool-isolation",deps)}},
  {"watermark",func()(PreparedCreateDelivery,error){return service.PreparePinnedWatermarkDelivery(context.Background(),who,tenant,request,"pool-isolation",deps)}},
 }
 for _,call:=range calls{t.Run(call.name,func(t *testing.T){_,err:=call.run();var denied *domain.Error;if !errors.As(err,&denied)||denied.Code!="DELIVERY_UNAVAILABLE"{t.Fatal("shared privileged pool reached dependency IO",err)}})}
}

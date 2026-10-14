package main

import (
 "context"
 "strings"
 "testing"
)

func TestDeliveryCreateUnconfiguredKeepsStartupAvailable(t *testing.T){
 t.Setenv("PHOTO_DELIVERY_METADATA_SOURCE_ENABLED","false")
 t.Setenv("V30_DELIVERY_SCHEDULER_DATABASE_URL","")
 service,close,err:=buildDeliveryCreate(context.Background(),nil,nil,"")
 if err!=nil||service!=nil||close==nil {t.Fatal("unconfigured delivery changed startup",err)}
 close()
}
func TestDeliveryCreateConfiguredRefusesMissingPortsWithoutLeakingDSN(t *testing.T){
 t.Setenv("V30_DELIVERY_SCHEDULER_DATABASE_URL","postgres://owner:secret@invalid/delivery")
 service,close,err:=buildDeliveryCreate(context.Background(),nil,nil,"documents")
 if err==nil||service!=nil||close==nil {t.Fatal("missing dependencies accepted")}
 if strings.Contains(err.Error(),"secret")||strings.Contains(err.Error(),"postgres://") {t.Fatal("credential leaked")}
 close()
}

func TestDeliveryCreateCancellationBeforeConfiguration(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 service,close,err:=buildDeliveryCreate(ctx,nil,nil,"")
 if err!=context.Canceled||service!=nil||close==nil{t.Fatal("cancelled startup continued",err)}
 close()
}

func TestDeliveryCreateRejectsInvalidMetadataInstallation(t *testing.T){
 t.Setenv("PHOTO_DELIVERY_METADATA_SOURCE_ENABLED","invalid")
 t.Setenv("V30_DELIVERY_SCHEDULER_DATABASE_URL","")
 service,close,err:=buildDeliveryCreate(context.Background(),nil,nil,"")
 if err==nil||service!=nil||close==nil{t.Fatal("invalid metadata installation accepted")};close()
}

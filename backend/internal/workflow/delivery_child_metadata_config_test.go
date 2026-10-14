package workflow

import (
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/delivery"
)
func TestMetadataChildConfigRejectsUnboundTask(t *testing.T){
 if _,_,err:=(FrozenDeliveryInitialTask{}).childConfig(uuid.New(),delivery.PreparePlan{},delivery.ExportChildIntent{},uuid.New());err==nil{t.Fatal("unbound task accepted")}
}

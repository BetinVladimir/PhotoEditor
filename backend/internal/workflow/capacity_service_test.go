package workflow

import (
 "context"
 "strings"
 "testing"
 "photoeditor/backend/internal/objectstore"
)

func TestCapacitySchedulerRejectsMalformedPinBeforeExternalIO(t *testing.T){
 t.Setenv("PHOTO_V32_SCHEDULER_POLICY_SHA256","bad")
 err:=RunSchedulerWithArchiveCompletion(context.Background(),"invalid-dsn","invalid-listen",objectstore.Config{},"missing-icc",nil,nil)
 if err==nil||!strings.Contains(err.Error(),"V32 scheduler policy SHA256"){t.Fatal("malformed pin reached external setup",err)}
}

package workflow
import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func TestReplacementDetectorAdoptionQueueRequiresDBAndActiveContext(t *testing.T){
 if _,err:=(ReplacementDetectionAdoptionQueue{}).Next(context.Background(),uuid.Nil);err!=v2input.ErrInvalid{t.Fatal("missing discovery DB accepted",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=(ReplacementDetectionAdoptionQueue{}).Next(ctx,uuid.Nil);err!=context.Canceled{t.Fatal("cancelled discovery accepted",err)}
}

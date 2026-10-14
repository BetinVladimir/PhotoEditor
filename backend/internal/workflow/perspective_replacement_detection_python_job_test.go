package workflow
import (
 "context"
 "testing"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
func TestReplacementDetectorPythonJobRejectsInvalidProcessConfiguration(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=(PythonReplacementDetectionAdopter{}).Run(ctx,ReplacementDetectionDocuments{},1,1,uuid.New(),v2input.Ref{});err!=context.Canceled{t.Fatal("cancelled process accepted",err)}
 if _,err:=(PythonReplacementDetectionAdopter{Python:"relative",WorkerRoot:"/workers",Timeout:time.Minute}).Run(context.Background(),ReplacementDetectionDocuments{},1,1,uuid.New(),v2input.Ref{});err!=v2input.ErrInvalid{t.Fatal("invalid process configuration accepted",err)}
}

package workflow
import (
 "context"
 "testing"
 "time"
 "photoeditor/backend/internal/v2input"
)
func TestReplacementDetectorBridgeJobRejectsInvalidLifecycleBeforeRunner(t *testing.T){
 called:=false
 runner:=func(context.Context,string,string)error{called=true;return nil}
 for _,job:=range []ReplacementDetectionBridgeJob{{Root:"relative",Timeout:time.Second},{Root:"/",Timeout:0},{Root:"/",Timeout:11*time.Minute}}{
  if err:=job.Run(context.Background(),ReplacementDetectionDocuments{},runner);err!=v2input.ErrInvalid{t.Fatal("invalid job accepted",err)}
 }
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=(ReplacementDetectionBridgeJob{Root:"/",Timeout:time.Second}).Run(ctx,ReplacementDetectionDocuments{},runner);err!=context.Canceled{t.Fatal("cancelled job accepted",err)}
 if called{t.Fatal("invalid lifecycle started runner")}
}

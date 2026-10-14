package workflow

import (
 "context"
 "errors"
 "testing"
)

func TestCapacityRuntimeInvalidStartupDoesNotAcquireConnection(t *testing.T){
 for _,pin:=range []string{"","bad","AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"}{
  if runtime,err:=NewCapacityRuntime(context.Background(),nil,pin);err==nil||runtime!=nil{t.Fatal("accepted invalid startup")}
 }
}
func TestCapacityRuntimeClosedInstanceCannotDispatch(t *testing.T){
 runtime:=&CapacityRuntime{}
 runtime.Close();runtime.Close()
 if _,err:=runtime.Dispatch(context.Background(),20);!errors.Is(err,ErrCapacityLeaderUnavailable){t.Fatal(err)}
}

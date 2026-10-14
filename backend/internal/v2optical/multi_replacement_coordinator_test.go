package v2optical

import (
 "context"
 "errors"
 "reflect"
 "testing"
 "github.com/google/uuid"
)

func TestMultiReplacementCoordinatorCancellationDuringReceipt(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());defer cancel()
 unexpected:=func(context.Context)error{t.Fatal("admitted task republished");return nil}
 got,err:=executeMultiReplacementPublication(ctx,multiReplacementPublicationSteps{
  probe:func(context.Context)(string,error){return "admitted",nil},
  params:unexpected,transport:unexpected,admit:unexpected,
  receipt:func(context.Context)(MultiReplacementReceipt,error){cancel();return MultiReplacementReceipt{Task:uuid.New()},nil},
 })
 if !errors.Is(err,context.Canceled)||got.Task!=uuid.Nil{t.Fatal("cancelled acknowledgement returned",err)}
}

func TestMultiReplacementCoordinatorResumeAndCommitAmbiguity(t *testing.T){
 dependency:=errors.New("dependency or ambiguous commit")
 for _,tc:=range []struct{state,fail string;want []string;success bool}{
  {"absent","",[]string{"probe","params","transport","admit","receipt"},true},
  {"draft","",[]string{"probe","params","transport","admit","receipt"},true},
  {"admitted","",[]string{"probe","receipt"},true},
  {"stale","",[]string{"probe"},false},
  {"absent","probe",[]string{"probe"},false},
  {"absent","params",[]string{"probe","params"},false},
  {"draft","transport",[]string{"probe","params","transport"},false},
  {"draft","admit",[]string{"probe","params","transport","admit"},false},
  {"admitted","receipt",[]string{"probe","receipt"},false},
 }{
  t.Run(tc.state+"/"+tc.fail,func(t *testing.T){
   calls:=[]string{}
   record:=func(name string)error{calls=append(calls,name);if tc.fail==name{return dependency};return nil}
   receipt:=MultiReplacementReceipt{Task:uuid.New()}
   got,err:=executeMultiReplacementPublication(context.Background(),multiReplacementPublicationSteps{
    probe:func(context.Context)(string,error){return tc.state,record("probe")},
    params:func(context.Context)error{return record("params")},
    transport:func(context.Context)error{return record("transport")},
    admit:func(context.Context)error{return record("admit")},
    receipt:func(context.Context)(MultiReplacementReceipt,error){return receipt,record("receipt")},
   })
   if !reflect.DeepEqual(calls,tc.want){t.Fatal("wrong publication/retry order",calls,tc.want)}
   if tc.success{if err!=nil||got.Task!=receipt.Task{t.Fatal("receipt missing",err)}}else{
    if err==nil||got.Task!=uuid.Nil{t.Fatal("failed publication returned acknowledgement",err)}
    if tc.fail!=""&&!errors.Is(err,dependency){t.Fatal("dependency error lost",err)}
   }
  })
 }
}

func TestMultiReplacementCoordinatorCancellationStopsFollowingPhases(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());defer cancel()
 calls:=[]string{}
 _,err:=executeMultiReplacementPublication(ctx,multiReplacementPublicationSteps{
  probe:func(context.Context)(string,error){calls=append(calls,"probe");return "draft",nil},
  params:func(context.Context)error{calls=append(calls,"params");cancel();return nil},
  transport:func(context.Context)error{t.Fatal("transport after cancellation");return nil},
  admit:func(context.Context)error{t.Fatal("admission after cancellation");return nil},
  receipt:func(context.Context)(MultiReplacementReceipt,error){t.Fatal("receipt after cancellation");return MultiReplacementReceipt{},nil},
 })
 if err!=context.Canceled||!reflect.DeepEqual(calls,[]string{"probe","params"}){t.Fatal("cancellation ignored",calls,err)}
}

func TestMultiReplacementCoordinatorRejectsMissingDependencies(t *testing.T){
 var prepared PreparedNegativeReplacementInputs
 if _,err:=prepared.ExecuteMultiReplacement(context.Background(),nil,nil,"photos",MultiReplacementExecutionRequest{});err==nil{t.Fatal("missing dependencies accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=prepared.ExecuteMultiReplacement(ctx,nil,nil,"photos",MultiReplacementExecutionRequest{});err!=context.Canceled{t.Fatal(err)}
 if _,err:=executeMultiReplacementPublication(context.Background(),multiReplacementPublicationSteps{});err==nil{t.Fatal("incomplete phases accepted")}
}

package delivery

import (
 "context"
 "errors"
 "reflect"
 "testing"
 "photoeditor/backend/internal/idempotency"
)

func TestCreateCoordinatorConcurrentCompletionReplay(t *testing.T){
 for _,tc:=range []struct{name string;initial,concurrent bool;prepareErr,recoverErr error;want []string;success bool}{
  {"fresh",false,false,nil,nil,[]string{"recover","prepare","activate"},true},
  {"completed",true,false,nil,nil,[]string{"recover","activate"},true},
  {"concurrent completion",false,true,errors.New("phase stale"),nil,[]string{"recover","prepare","recover","activate"},true},
  {"incomplete",false,false,errors.New("storage unavailable"),nil,[]string{"recover","prepare","recover"},false},
  {"authorization lost",false,true,errors.New("phase stale"),errors.New("forbidden"),[]string{"recover","prepare","recover"},false},
 } {
  t.Run(tc.name,func(t *testing.T){
   var calls []string;reads:=0
   out,err:=coordinateCreateDelivery(context.Background(),createDeliveryPhases{
    recover:func(context.Context)(CreateRecovery,error){calls=append(calls,"recover");reads++;if reads==1{return CreateRecovery{Completed:tc.initial},nil};return CreateRecovery{Completed:tc.concurrent},tc.recoverErr},
    prepare:func(context.Context)error{calls=append(calls,"prepare");return tc.prepareErr},
    activate:func(context.Context)(idempotency.Response,error){calls=append(calls,"activate");return idempotency.Response{Status:202,Replayed:tc.initial||tc.concurrent},nil},
   })
   if !reflect.DeepEqual(calls,tc.want)||(err==nil)!=tc.success {t.Fatal(calls,err)}
   if tc.success {if out.Status!=202||out.Replayed!=(tc.initial||tc.concurrent){t.Fatal("receipt changed")}} else {
    expected:=tc.prepareErr;if tc.recoverErr!=nil{expected=tc.recoverErr};if !errors.Is(err,expected)||out.Status!=0{t.Fatal("failure cause lost",err)}
   }
  })
 }
}
func TestCreateCoordinatorCancellationStopsRecoveryAndActivation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());calls:=0
 out,err:=coordinateCreateDelivery(ctx,createDeliveryPhases{
  recover:func(context.Context)(CreateRecovery,error){calls++;return CreateRecovery{},nil},
  prepare:func(context.Context)error{cancel();return errors.New("stale")},
  activate:func(context.Context)(idempotency.Response,error){t.Fatal("cancelled activation");return idempotency.Response{},nil},
 })
 if !errors.Is(err,context.Canceled)||calls!=1||out.Status!=0{t.Fatal("cancelled request recovered",calls,err)}
}

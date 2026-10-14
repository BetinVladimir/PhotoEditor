package workflow

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2optical"
)
func TestAutomaticPublicationRecoveryDoesNotRepeatMutations(t *testing.T){
 for _,state:=range []string{"absent","draft","admitted"}{t.Run(state,func(t *testing.T){
  mutations:=0;reads:=0
  phase:=func(context.Context)error{mutations++;return nil}
  steps:=automaticPerspectivePublicationSteps{probe:func(context.Context)(string,error){return state,nil},save:phase,params:phase,transport:phase,admit:phase,receipt:func(context.Context)(v2optical.AutomaticPerspectiveReplacementReceipt,error){reads++;return v2optical.AutomaticPerspectiveReplacementReceipt{Task:uuid.New()},nil}}
  receipt,err:=executeAutomaticPerspectivePublication(context.Background(),steps)
  expected:=4;if state=="admitted"{expected=0}
  if err!=nil||mutations!=expected||reads!=1||receipt.RecoveredExisting!=(state=="admitted"){t.Fatal(mutations,reads,receipt,err)}
 })}
}
func TestAutomaticPublicationAmbiguousCommitUsesOnlyReceipt(t *testing.T){
 failure:=errors.New("commit response lost");mutations:=0;reads:=0
 phase:=func(context.Context)error{mutations++;return nil}
 steps:=automaticPerspectivePublicationSteps{probe:func(context.Context)(string,error){return "draft",nil},save:phase,params:phase,transport:phase,admit:func(context.Context)error{mutations++;return failure},receipt:func(context.Context)(v2optical.AutomaticPerspectiveReplacementReceipt,error){reads++;return v2optical.AutomaticPerspectiveReplacementReceipt{Task:uuid.New()},nil}}
 receipt,err:=executeAutomaticPerspectivePublication(context.Background(),steps)
 if err!=nil||!receipt.RecoveredExisting||mutations!=4||reads!=1{t.Fatal(receipt,err,mutations,reads)}
 steps.receipt=func(context.Context)(v2optical.AutomaticPerspectiveReplacementReceipt,error){return v2optical.AutomaticPerspectiveReplacementReceipt{},errors.New("not confirmed")}
 if _,err:=executeAutomaticPerspectivePublication(context.Background(),steps);!errors.Is(err,failure){t.Fatal("original ambiguity lost",err)}
}

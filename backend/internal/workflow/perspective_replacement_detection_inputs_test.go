package workflow

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func TestReplacementDetectionInputsRequireIOAndExactDescriptor(t *testing.T){
 descriptor,detector:=replacementDetectionDescriptorFixture(t)
 raw,err:=canonical.Marshal(descriptor);if err!=nil{t.Fatal(err)}
 plan:=PerspectiveReplacementDetectionPlan{detector:detector,descriptor:string(raw)}
 snapshot:=uuid.New()
 if _,err=plan.VerifyInputs(context.Background(),snapshot,nil,nil,nil);err!=v2input.ErrInvalid{t.Fatal("missing IO accepted",err)}
 if _,err=plan.VerifyInputs(context.Background(),uuid.Nil,nil,nil,nil);err!=v2input.ErrInvalid{t.Fatal("missing snapshot accepted",err)}
 witness:=VerifiedReplacementDetectionInputs{descriptor:plan.descriptor,snapshot:snapshot}
 if _,err=witness.AdmissionJSON(context.Background(),plan,snapshot,"photos");err!=v2input.ErrInvalid{t.Fatal("unverified witness accepted",err)}
 witness.descriptor="changed"
 if _,err=witness.AdmissionJSON(context.Background(),plan,snapshot,"photos");err!=v2input.ErrInvalid{t.Fatal("foreign descriptor accepted",err)}
 witness.descriptor=plan.descriptor
 if _,err=witness.AdmissionJSON(context.Background(),plan,uuid.New(),"photos");err!=v2input.ErrInvalid{t.Fatal("foreign snapshot accepted",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err=plan.VerifyInputs(ctx,snapshot,nil,nil,nil);err!=context.Canceled{t.Fatal("cancelled IO accepted",err)}
 if _,err=witness.AdmissionJSON(ctx,plan,snapshot,"photos");err!=context.Canceled{t.Fatal("cancelled admission accepted",err)}
}

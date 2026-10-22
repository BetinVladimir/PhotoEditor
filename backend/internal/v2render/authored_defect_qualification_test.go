package v2render

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

func TestAuthoredDefectExpectedOwnsEveryMutablePolicyCollection(t *testing.T){
 expected:=v2qualification.Expected{Operation:"defects.propose",Version:"1",Backend:"cpu",RequiredChecks:[]string{"quality"},CalibrationSHAs:map[string]struct{}{"calibration":{}},TestFixtureSHAs:map[string]struct{}{"fixture":{}},EvidenceSHAs:map[string]struct{}{"evidence":{}}}
 owned:=ownAuthoredDefectExpected(expected)
 before,err:=canonical.Marshal(owned);if err!=nil{t.Fatal(err)}
 expected.RequiredChecks[0]="other";delete(expected.CalibrationSHAs,"calibration");expected.CalibrationSHAs["other"]=struct{}{}
 delete(expected.TestFixtureSHAs,"fixture");expected.TestFixtureSHAs["other"]=struct{}{}
 delete(expected.EvidenceSHAs,"evidence");expected.EvidenceSHAs["other"]=struct{}{}
 after,err:=canonical.Marshal(owned);if err!=nil||string(before)!=string(after){t.Fatal("caller mutation changed owned verification policy",err)}
 // Ownership is bidirectional, not just a copy of the outer Expected struct.
 owned.RequiredChecks[0]="owned";owned.EvidenceSHAs["owned"]=struct{}{}
 if expected.RequiredChecks[0]!="other"{t.Fatal("owned checks alias caller")}
 if _,exists:=expected.EvidenceSHAs["owned"];exists{t.Fatal("owned evidence aliases caller")}
 nilExpected:=ownAuthoredDefectExpected(v2qualification.Expected{})
 if nilExpected.CalibrationSHAs!=nil||nilExpected.TestFixtureSHAs!=nil||nilExpected.EvidenceSHAs!=nil||nilExpected.RequiredChecks!=nil{t.Fatal("nil collections changed")}
}
func TestAuthoredDefectQualificationCancellationPrecedesSQLAndObjectIO(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 s:=SQLAuthoredDefectQualification{}
 if _,err:=s.Read(ctx,auth.Identity{},uuid.Nil,uuid.Nil,v2qualification.Expected{},v2input.Ref{});!errors.Is(err,context.Canceled){t.Fatal("Read lost cancellation",err)}
 if err:=s.Recheck(ctx,auth.Identity{},uuid.Nil,uuid.Nil,v2qualification.Expected{},AuthoredDefectQualification{});!errors.Is(err,context.Canceled){t.Fatal("Recheck lost cancellation",err)}
}

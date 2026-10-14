package v2optical

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)
func TestAutomaticPerspectiveReplacementCompilerRequiresTransaction(t *testing.T){
 var step PreparedMappedReplacementStep
 if _,err:=step.CompileLockedAutomaticPerspectiveReplacement(context.Background(),nil,uuid.New(),LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},1,PerspectiveResourceEstimate{},v2input.Ref{},[]byte("{}"));err==nil{t.Fatal("missing transaction accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=step.CompileLockedAutomaticPerspectiveReplacement(ctx,nil,uuid.New(),LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},1,PerspectiveResourceEstimate{},v2input.Ref{},[]byte("{}"));err!=context.Canceled{t.Fatal(err)}
}

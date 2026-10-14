package v2optical

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2qualification"
)

func TestManualPerspectiveReplacementLockRequiresTransaction(t *testing.T){
 var p PreparedNegativeReplacementInputs
 draft,err:=p.LockManualPerspectiveReplacementDraft(context.Background(),nil,nil,nil,nil,nil,[]byte(`{}`),uuid.New(),LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},100,PerspectiveResourceEstimate{})
 if err==nil||len(draft.Bytes())!=0{t.Fatal("missing transaction accepted")}
}

func TestManualPerspectiveReplacementLockHonorsCancellation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 var p PreparedNegativeReplacementInputs
 draft,err:=p.LockManualPerspectiveReplacementDraft(ctx,nil,nil,nil,nil,nil,nil,uuid.Nil,LocatedPerspectiveQualification{},v2qualification.DeploymentLocks{},0,PerspectiveResourceEstimate{})
 if !errors.Is(err,context.Canceled)||len(draft.Bytes())!=0{t.Fatalf("cancellation ignored: %v",err)}
}

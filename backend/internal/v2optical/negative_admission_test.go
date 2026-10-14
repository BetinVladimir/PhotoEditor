package v2optical

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

func TestNegativeAdmissionRejectsMissingDependenciesBeforeIO(t *testing.T) {
 _,err:=AdmitNegativeGraph(context.Background(),nil,nil,"bucket",auth.Identity{},uuid.Nil,NegativeDraft{},LocatedNegativeQualification{},v2input.Ref{})
 if err!=v2input.ErrInvalid { t.Fatalf("got %v",err) }
}

func TestNegativeAdmissionCancellationBeforeIO(t *testing.T) {
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 _,err:=AdmitNegativeGraph(ctx,nil,nil,"bucket",auth.Identity{},uuid.Nil,NegativeDraft{},LocatedNegativeQualification{},v2input.Ref{})
 if err!=context.Canceled { t.Fatalf("got %v",err) }
}

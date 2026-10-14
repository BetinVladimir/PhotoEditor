package v2optical

import (
 "context"
 "errors"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2qualification"
)

func TestPerspectiveReplacementDraftRequiresPhysicalAuthority(t *testing.T){
 var step PreparedMappedReplacementStep
 if _,err:=step.CompilePerspectiveReplacementDraft(context.Background(),uuid.New(),PerspectiveQualificationPin{},v2qualification.DeploymentLocks{},nil,nil,100,PerspectiveResourceEstimate{},nil);err==nil{t.Fatal("unproved replacement step accepted")}
}

func TestPerspectiveReplacementDraftRestoreRejectsUnscopedPackets(t *testing.T){
 var step PreparedMappedReplacementStep
 for _,raw:=range [][]byte{nil,[]byte(`{}`),[]byte(`{"schema_version":1}`),[]byte(`{"schema_version":2,"draft":{}}`),make([]byte,(3<<20)+1)}{
  if _,err:=step.RestorePerspectiveReplacementDraft(context.Background(),raw,uuid.New(),PerspectiveQualificationPin{},v2qualification.DeploymentLocks{},nil,nil,100,PerspectiveResourceEstimate{},nil);err==nil{t.Fatal("unscoped replacement packet accepted")}
 }
}

func TestPerspectiveReplacementDraftCancellation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 var step PreparedMappedReplacementStep
 if _,err:=step.CompilePerspectiveReplacementDraft(ctx,uuid.New(),PerspectiveQualificationPin{},v2qualification.DeploymentLocks{},nil,nil,100,PerspectiveResourceEstimate{},nil);!errors.Is(err,context.Canceled){t.Fatal(err)}
 if _,err:=step.RestorePerspectiveReplacementDraft(ctx,[]byte(`{}`),uuid.New(),PerspectiveQualificationPin{},v2qualification.DeploymentLocks{},nil,nil,100,PerspectiveResourceEstimate{},nil);!errors.Is(err,context.Canceled){t.Fatal(err)}
}

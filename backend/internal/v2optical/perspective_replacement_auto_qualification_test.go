package v2optical

import (
 "context"
 "testing"
 "photoeditor/backend/internal/v2input"
)
func TestAutomaticReplacementQualificationCancellation(t *testing.T){
 var projects *PerspectiveProjects
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=projects.PrepareAutomaticReplacementQualification(ctx,PreparedMappedReplacementStep{},v2input.Ref{},nil);err!=context.Canceled{t.Fatal(err)}
}

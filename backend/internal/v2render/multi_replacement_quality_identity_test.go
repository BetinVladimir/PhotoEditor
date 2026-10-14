package v2render
import (
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)
func TestMultiReplacementQualityAndPreviewIdentity(t *testing.T){
 for _,op:=range []string{"optical.panorama","optical.focus_stack"}{t.Run(op,func(t *testing.T){
 b,_,_:=multiReadFixture(t,op)
 i:=MultiReplacementBindingIdentity{Tenant:uuid.New(),Project:uuid.New(),Review:uuid.New(),DecisionHash:strings.Repeat("d",64),OriginalRun:uuid.New(),OriginalTask:uuid.New()}
 namespace:=uuid.NewSHA1(i.Tenant,[]byte("V29.NegativeReplacement/v2:"+i.Project.String()+":"+i.Review.String()+":"+i.DecisionHash))
 b.Run=uuid.NewSHA1(namespace,[]byte("run:"+i.OriginalRun.String()+":"+i.OriginalTask.String()));b.Workflow,_=v2input.WorkflowID(b.Run);b.Replacement=i
 source:=MultiPreviewSource{Tenant:i.Tenant,Project:i.Project,Producer:uuid.NewSHA1(namespace,[]byte("task:"+i.OriginalTask.String())),Binding:b,Attempt:1,Fence:2,Principal:auth.Identity{UserID:uuid.New(),SessionID:uuid.New()}}
 quality,err:=MultiQualityRunID(source);if err!=nil{t.Fatal(err)}
 preview,err:=MultiPreviewRunID(source,strings.Repeat("a",64));if err!=nil||preview==quality{t.Fatal("invalid preview/quality identity",err)}
 changed:=source;changed.Fence++
 next,err:=MultiQualityRunID(changed);if err!=nil||next==quality{t.Fatal("quality fence lost",err)}
 for _,alter:=range []func(*MultiPreviewSource){func(s *MultiPreviewSource){s.Tenant=uuid.New()},func(s *MultiPreviewSource){s.Project=uuid.New()},func(s *MultiPreviewSource){s.Binding.Replacement=MultiReplacementBindingIdentity{}},func(s *MultiPreviewSource){s.Binding.Replacement.OriginalTask=uuid.New()}}{changed:=source;alter(&changed);if _,err:=MultiQualityRunID(changed);err==nil{t.Fatal("foreign quality source accepted")};if _,err:=MultiPreviewRunID(changed,strings.Repeat("a",64));err==nil{t.Fatal("foreign preview source accepted")}}
 })}
}

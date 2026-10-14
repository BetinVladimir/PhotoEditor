package v2render
import (
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestMultiReplacementBindingIdentity(t *testing.T){
 for _,operation:=range []string{"optical.panorama","optical.focus_stack"}{t.Run(operation,func(t *testing.T){
 b,_,_:=multiReadFixture(t,operation)
 identity:=MultiReplacementBindingIdentity{Tenant:uuid.New(),Project:uuid.New(),Review:uuid.New(),DecisionHash:strings.Repeat("d",64),OriginalRun:uuid.New(),OriginalTask:uuid.New()}
 namespace:=uuid.NewSHA1(identity.Tenant,[]byte("V29.NegativeReplacement/v2:"+identity.Project.String()+":"+identity.Review.String()+":"+identity.DecisionHash))
 b.Run=uuid.NewSHA1(namespace,[]byte("run:"+identity.OriginalRun.String()+":"+identity.OriginalTask.String()));b.Workflow,_=v2input.WorkflowID(b.Run)
 producer:=uuid.NewSHA1(namespace,[]byte("task:"+identity.OriginalTask.String()));b.Replacement=identity
 if err:=validateAdoptedMultiBinding(b,producer,operation,b.Result);err!=nil{t.Fatal(err)}
 raw,_:=canonical.Marshal(identity);decoded,err:=decodeMultiReplacementBindingIdentity(raw);if err!=nil||decoded!=identity{t.Fatal("identity lost",err)}
 for _,alter:=range []func(*MultiReplacementBindingIdentity){func(i *MultiReplacementBindingIdentity){i.Tenant=uuid.New()},func(i *MultiReplacementBindingIdentity){i.Project=uuid.New()},func(i *MultiReplacementBindingIdentity){i.Review=uuid.New()},func(i *MultiReplacementBindingIdentity){i.DecisionHash=strings.Repeat("e",64)},func(i *MultiReplacementBindingIdentity){i.OriginalRun=uuid.New()},func(i *MultiReplacementBindingIdentity){i.OriginalTask=uuid.New()}}{changed:=b;alter(&changed.Replacement);if err:=validateAdoptedMultiBinding(changed,producer,operation,b.Result);err==nil{t.Fatal("identity substitution accepted")}}
 b.Replacement=MultiReplacementBindingIdentity{};if err:=validateAdoptedMultiBinding(b,producer,operation,b.Result);err==nil{t.Fatal("replacement without identity accepted")}
 })}
 for _,raw:=range []string{`{}`,`{"unknown":true}`,`[]`}{if _,err:=decodeMultiReplacementBindingIdentity([]byte(raw));err==nil{t.Fatal("malformed identity accepted")}}
}

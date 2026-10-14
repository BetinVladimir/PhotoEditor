package v2render
import (
 "encoding/json"
 "strings"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)
func TestMultiReplacementOriginalInventoryPreservesPrivateIdentity(t *testing.T){
 for _,operation:=range []string{"optical.panorama","optical.focus_stack"}{t.Run(operation,func(t *testing.T){
 binding,_,inv,_:=originalInventoryFixture(t,operation)
 identity:=MultiReplacementBindingIdentity{Tenant:uuid.New(),Project:uuid.New(),Review:uuid.New(),DecisionHash:strings.Repeat("d",64),OriginalRun:uuid.New(),OriginalTask:uuid.New()}
 namespace:=uuid.NewSHA1(identity.Tenant,[]byte("V29.NegativeReplacement/v2:"+identity.Project.String()+":"+identity.Review.String()+":"+identity.DecisionHash))
 binding.Run=uuid.NewSHA1(namespace,[]byte("run:"+identity.OriginalRun.String()+":"+identity.OriginalTask.String()));binding.Workflow,_=v2input.WorkflowID(binding.Run);binding.Replacement=identity
 task:=uuid.NewSHA1(namespace,[]byte("task:"+identity.OriginalTask.String()));inv.binding=binding
 raw,_:=canonical.Marshal(originalInventoryWire(inv))
 decoded,err:=decodeMultiOriginalInventory(raw,binding,task,"photos");if err!=nil||!decoded.equal(inv){t.Fatal("private identity lost",err)}
 changed:=binding;changed.Replacement.Review=uuid.New();if _,err=decodeMultiOriginalInventory(raw,changed,task,"photos");err==nil{t.Fatal("foreign expected identity accepted")}
 var packet map[string]any;json.Unmarshal(raw,&packet);packet["binding"].(map[string]any)["replacement"]=identity
 injected,_:=canonical.Marshal(packet);if _,err=decodeMultiOriginalInventory(injected,binding,task,"photos");err==nil{t.Fatal("inventory supplied identity accepted")}
 })}
}

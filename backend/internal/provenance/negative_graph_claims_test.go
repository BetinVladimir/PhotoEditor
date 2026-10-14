package provenance

import (
 "context"
 "errors"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

func TestNegativeGraphContentOutputsRejectGeneratedAndAliasedChildren(t *testing.T){
 sha:=strings.Repeat("a",64)
 pin:=func()v2input.Ref{return v2input.Ref{ArtifactID:uuid.New(),SHA256:sha}}
 root:=pin()
 original:=SigningExecutionNode{Outputs:[]v2input.Ref{root},Action:SigningAction{Operation:"optical.negative",Version:"1"},Children:[]SigningChildOutput{{Root:root,Ref:pin(),Role:"working_image"},{Root:root,Ref:pin(),Role:"changed_mask"},{Root:root,Ref:pin(),Role:"report"}}}
 for _,mode:=range []string{"valid","generated","alias","wrong_root","missing_report","duplicate_role","unknown_operation"}{
  t.Run(mode,func(t *testing.T){
   node:=original;node.Children=append([]SigningChildOutput(nil),original.Children...)
   switch mode{
   case "generated":node.Children=append(node.Children,SigningChildOutput{Root:root,Ref:pin(),Role:"generated_mask"})
   case "alias":node.Children[0].Ref=root
   case "wrong_root":node.Children[1].Root=pin()
   case "missing_report":node.Children=node.Children[:2]
   case "duplicate_role":node.Children[2].Role="working_image"
   case "unknown_operation":node.Action.Operation="optical.unknown"
   }
   outputs,err:=signingNodeContentOutputs(node)
   if mode=="valid"{if err!=nil||len(outputs)!=4{t.Fatal("valid negative children refused",err)};return}
   if err==nil{t.Fatal("invalid negative children accepted",mode)}
  })
 }
}

func TestNegativeGraphResolverRequiresAuthorityAndPreservesCancellation(t *testing.T){
 resolver:=NegativeSigningTaskResolver{}
 if _,err:=resolver.ResolveSigningTaskLineage(context.Background(),auth.Identity{},ReadySigningExport{},CanonicalSigningTask{});err==nil{t.Fatal("missing causal authority accepted")}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=resolver.ResolveSigningTaskLineage(ctx,auth.Identity{},ReadySigningExport{},CanonicalSigningTask{});!errors.Is(err,context.Canceled){t.Fatal("cancellation lost",err)}
}

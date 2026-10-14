package workflow

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

func TestSavedWatermarkParamsAcceptOnlyExactDescriptorRef(t *testing.T){
 child:=delivery.ExportChildIntent{RunID:uuid.New()};workflow,err:=v2input.WorkflowID(child.RunID);if err!=nil{t.Fatal(err)}
 for _,op:=range []string{"derivative.sharpen","derivative.watermark","export.encode"}{t.Run(op,func(t *testing.T){
  id,err:=v2input.TaskID(workflow,op,0);if err!=nil{t.Fatal(err)}
  node:=CompiledDownloadNode{ID:id,Operation:op,Params:[]byte(`{"frozen":true}`)}
  expected:=v2input.Ref{ArtifactID:uuid.New(),SHA256:renderDerivativeBytesSHA(node.Params)}
  raw,err:=canonical.Marshal(expected);if err!=nil{t.Fatal(err)}
  for _,scenario:=range []string{"valid","absent","wrong-sha","unknown","trailing"}{
   body:=append([]byte(nil),raw...)
   switch scenario{
   case "absent":body=[]byte("null")
   case "wrong-sha":changed:=expected;changed.SHA256=renderDerivativeBytesSHA([]byte("different"));body,_=canonical.Marshal(changed)
   case "unknown":body=append(append([]byte{},raw[:len(raw)-1]...),[]byte(`,"unexpected":true}`)...)
   case "trailing":body=append(body,[]byte(` {}`)...)
   }
   db:=&deliveryStageReadDB{responses:[][]byte{body}}
   ref,err:=ReadSavedWatermarkContinuationParams(context.Background(),db,delivery.ExpansionLease{},child,node)
   if scenario=="valid"{if err!=nil||ref==nil||*ref!=expected{t.Fatal(scenario,ref,err)}}else if scenario=="absent"{if err!=nil||ref!=nil{t.Fatal(scenario,ref,err)}}else if err==nil{t.Fatal("malformed recovery accepted",scenario)}
  }
 })}
}

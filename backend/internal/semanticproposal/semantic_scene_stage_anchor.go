package semanticproposal

import (
 "context"
 "encoding/json"

 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

// Identity is derived from the full immutable version binding, not an ordinal
// or "latest analysis" lookup. ReuseReference therefore names the same child.
type semanticSceneStageAnchor struct {
 tenant,project,actor,session,parent uuid.UUID
 source v2input.SessionVersionBinding
 operation,requestSHA,bindingSHA,sourceBody string
 namespace,run,workflow,task,document uuid.UUID
}

func compileSemanticSceneStageAnchor(ctx context.Context,who auth.Identity,project uuid.UUID,request Request,binding v2input.SessionVersionBinding,image json.RawMessage,operation string)(semanticSceneStageAnchor,error){
 var zero semanticSceneStageAnchor
 if err:=ctx.Err();err!=nil{return zero,err}
 if project==uuid.Nil || who.UserID==uuid.Nil || who.SessionID==uuid.Nil || request.TenantID==uuid.Nil || request.RunID==uuid.Nil || !validSceneProducerOperation(operation){return zero,v2input.ErrInvalid}
 if _,err:=domain.ParseDigest(request.RequestSHA);err!=nil{return zero,v2input.ErrInvalid}
 owned:=append(json.RawMessage(nil),image...)
 if _,err:=v2input.ResolveSemanticSourceMetadata(binding,owned);err!=nil{return zero,err}
 digest,err:=canonical.Hash(binding)
 if err!=nil{return zero,err}
 namespace:=uuid.NewSHA1(request.RunID,[]byte("semantic.scene.source:"+digest))
 run:=uuid.NewSHA1(namespace,[]byte("semantic.scene."+operation+".run"))
 workflow,err:=v2input.WorkflowID(run)
 if err!=nil{return zero,err}
 task,err:=v2input.TaskID(workflow,operation,0)
 if err!=nil{return zero,err}
 document:=uuid.NewSHA1(namespace,[]byte("semantic.scene."+operation+".params"))
 if err:=ctx.Err();err!=nil{return zero,err}
 return semanticSceneStageAnchor{tenant:request.TenantID,project:project,actor:who.UserID,session:who.SessionID,parent:request.RunID,
  source:binding,operation:operation,requestSHA:request.RequestSHA,bindingSHA:digest,sourceBody:string(owned),namespace:namespace,run:run,workflow:workflow,task:task,document:document},nil
}

func verifySemanticSceneStageAnchor(ctx context.Context,who auth.Identity,project uuid.UUID,request Request,anchor semanticSceneStageAnchor)error{
 rebuilt,err:=compileSemanticSceneStageAnchor(ctx,who,project,request,anchor.source,json.RawMessage(anchor.sourceBody),anchor.operation)
 if err!=nil{return err}
 if rebuilt!=anchor{return v2input.ErrConflict}
 return ctx.Err()
}

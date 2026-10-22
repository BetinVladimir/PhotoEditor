package semanticproposal

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type semanticSceneStageParams struct {
 anchor semanticSceneStageAnchor
 ref v2input.Ref
 body string
 qualification semanticSceneProducerProof
 source sourcePreparationProof
}

// Physical source/ICC and independent producer qualification bracket object IO.
// The comparable proof is retained for the later admission transaction; an
// immutable published Params document by itself is never admission authority.
func (s SQLTransferSourceReader) publishSemanticSceneStageParams(ctx context.Context,who auth.Identity,request Request,anchor semanticSceneStageAnchor,color TransferColorProfile,publisher v2draft.Publisher,producer TransferSceneProducer)(semanticSceneStageParams,error){
 var zero semanticSceneStageParams
 if err:=verifySemanticSceneStageAnchor(ctx,who,s.ProjectID,request,anchor);err!=nil{return zero,err}
 if producer.Expected.Operation!=anchor.operation{return zero,v2input.ErrInvalid}
 qualification,err:=s.readSceneProducerQualification(ctx,who,request.TenantID,producer)
 if err!=nil{return zero,err}
 _,sourceProof,err:=s.readQualifiedSourcePreparationProof(ctx,who,request.TenantID,anchor.source,color,true)
 if err!=nil{return zero,err}
 image,err:=s.Read(ctx,who,request.TenantID,anchor.source)
 if err!=nil{return zero,err}
 if string(image)!=anchor.sourceBody{return zero,v2input.ErrConflict}
 prepared,err:=v2input.PrepareSemanticAnalysisParams(ctx,anchor.operation,anchor.source,image,producer.Expected.ModelLockSHA)
 if err!=nil{return zero,err}
 ref,err:=publisher.PublishSemanticAnalysisParams(ctx,who,request.TenantID,anchor.document,anchor.operation,anchor.source,json.RawMessage(anchor.sourceBody),producer.Expected.ModelLockSHA)
 if err!=nil{return zero,err}
 digest,err:=canonical.Hash(prepared.Params)
 if err!=nil || ref.ArtifactID==uuid.Nil || ref.SHA256!=digest{return zero,v2input.ErrConflict}
 if err:=s.recheckSourcePreparationProof(ctx,who,request.TenantID,anchor.source,color,true,sourceProof);err!=nil{return zero,err}
 current,err:=s.readSceneProducerQualification(ctx,who,request.TenantID,producer)
 if err!=nil{return zero,err}
 if current!=qualification{return zero,v2input.ErrConflict}
 if err:=ctx.Err();err!=nil{return zero,err}
 return semanticSceneStageParams{anchor:anchor,ref:ref,body:string(prepared.Params),qualification:qualification,source:sourceProof},nil
}

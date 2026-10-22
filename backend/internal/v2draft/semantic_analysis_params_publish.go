package v2draft

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
)

// PublishSemanticAnalysisParams uses the accepted-version publication protocol;
// it never accepts caller-authored model Params or replaces an existing intent.
// Qualification and source locks are checked again by task admission.
func (p Publisher) PublishSemanticAnalysisParams(ctx context.Context, who auth.Identity, tenant, document uuid.UUID, operation string, binding v2input.SessionVersionBinding, image json.RawMessage, modelLock string) (v2input.Ref,error) {
 if err:=ctx.Err();err!=nil{return v2input.Ref{},err}
 if tenant==uuid.Nil || document==uuid.Nil || who.UserID==uuid.Nil || who.SessionID==uuid.Nil{return v2input.Ref{},v2input.ErrInvalid}
 prepared,err:=v2input.PrepareSemanticAnalysisParams(ctx,operation,binding,image,modelLock)
 if err!=nil{return v2input.Ref{},err}
 for _,entry:=range prepared.Inputs {if entry.Ref.ArtifactID==document{return v2input.Ref{},v2input.ErrInvalid}}
 return p.publish(ctx,who,tenant,binding.AssetVersionID,document,prepared.Params,false,true)
}

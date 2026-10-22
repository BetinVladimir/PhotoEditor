package v2draft

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/stylevalidation"
 "photoeditor/backend/internal/v2input"
)

// PublishEditorExecutedStyle reuses the immutable accepted-source document
// reservation protocol (quota, upload fence, recovery, final source recheck).
// Publication alone does not authorize execution or replace frozen origin proof.
func(p Publisher)PublishEditorExecutedStyle(ctx context.Context,who auth.Identity,tid,version,document uuid.UUID,raw []byte)(v2input.Ref,error){
 var fields map[string]json.RawMessage
 if json.Unmarshal(raw,&fields)!=nil{return v2input.Ref{},domain.NewError("VALIDATION_FAILED",422)}
 for _,name:=range []string{"strength","global","local","beauty","defects","geometry","extensions"}{if !stylevalidation.Module(name,fields[name]){return v2input.Ref{},domain.NewError("VALIDATION_FAILED",422)}}
 return p.publish(ctx,who,tid,version,document,raw,false,true)
}

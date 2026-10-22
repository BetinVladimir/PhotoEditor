package semanticproposal

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

// Accepted-only recovery has no object/producer dependency. Unaccepted commands
// must restore their complete ordinary anchor through the original path.
func(s SQLTransferSourceReader) readOrdinaryParentReceiptRequest(ctx context.Context,who auth.Identity,tid,parent uuid.UUID)(Request,string,bool,error){
 var zero Request
 if err:=ctx.Err();err!=nil{return zero,"",false,err}
 if s.DB==nil || s.ProjectID==uuid.Nil || parent==uuid.Nil{return zero,"",false,v2input.ErrInvalid}
 var raw []byte
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  return tx.QueryRow(ctx,"SELECT photo.ordinary_parent_accepted_receipt_locator($1,$2,$3,$4,$5)",tid,s.ProjectID,who.UserID,who.SessionID,parent).Scan(&raw)
 })
 if err!=nil{return zero,"",false,err}
 if len(raw)==0{return zero,"",false,ctx.Err()}
 request,key,found,err:=decodeOrdinaryParentReceipt(raw,who,tid,parent)
 if err!=nil{return zero,"",false,err}
 if err:=ctx.Err();err!=nil{return zero,"",false,err}
 return request,key,found,nil
}

func decodeOrdinaryParentReceipt(raw []byte,who auth.Identity,tid,parent uuid.UUID)(Request,string,bool,error){
 var zero Request
 var fields map[string]json.RawMessage
 var saved struct{Key string `json:"key"`;SHA string `json:"request_sha"`;Run uuid.UUID `json:"run_id"`;Workflow uuid.UUID `json:"workflow_id"`;Config json.RawMessage `json:"config_snapshot"`;ConfigSHA string `json:"config_sha256"`}
 if len(raw)>8<<20 || json.Unmarshal(raw,&fields)!=nil || len(fields)!=6 || json.Unmarshal(raw,&saved)!=nil || !idempotency.ValidKey(saved.Key){return zero,"",false,v2input.ErrInvalid}
 for _,key:=range []string{"key","request_sha","run_id","workflow_id","config_snapshot","config_sha256"}{if len(fields[key])==0 || string(fields[key])=="null"{return zero,"",false,v2input.ErrInvalid}}
 if len(saved.Config)<2 || saved.Config[0]!='{'{return zero,"",false,v2input.ErrInvalid}
 if _,err:=domain.ParseDigest(saved.ConfigSHA);err!=nil{return zero,"",false,v2input.ErrInvalid}
 actual,err:=canonical.Hash(saved.Config)
 if err!=nil || actual!=saved.ConfigSHA{return zero,"",false,v2input.ErrConflict}
 run,err:=v2input.RunID(tid,who.UserID,saved.Key);if err!=nil{return zero,"",false,err}
 workflow,err:=v2input.WorkflowID(run);if err!=nil{return zero,"",false,err}
 if _,err:=domain.ParseDigest(saved.SHA);err!=nil || saved.Run!=parent || run!=parent || saved.Workflow!=workflow{return zero,"",false,v2input.ErrConflict}
 return Request{TenantID:tid,ActorID:who.UserID,RunID:parent,WorkflowID:workflow,RequestSHA:saved.SHA},saved.Key,true,nil
}

package v2render

import (
 "bytes"
 "context"
 "encoding/json"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
)

type MultiRunReviewScope struct {
 Tenant uuid.UUID `json:"tenant_id"`
 Project uuid.UUID `json:"project_id"`
 ReferenceAsset uuid.UUID `json:"reference_asset_id"`
 Run uuid.UUID `json:"run_id"`
 Workflow uuid.UUID `json:"workflow_id"`
 Task uuid.UUID `json:"task_id"`
 Operation string `json:"operation"`
 Intent json.RawMessage `json:"intent"`
}

func decodeMultiRunReviewScope(raw []byte,t,run uuid.UUID)(MultiRunReviewScope,error){
 var zero,value MultiRunReviewScope
 if len(raw)<2||len(raw)>70000{return zero,v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(raw);if err!=nil{return zero,v2input.ErrInvalid}
 if json.Unmarshal(normalized,&value)!=nil{return zero,v2input.ErrInvalid}
 exact,err:=canonical.Marshal(value)
 if err!=nil||!bytes.Equal(exact,normalized)||t==uuid.Nil||run==uuid.Nil||value.Tenant!=t||value.Run!=run||value.Project==uuid.Nil||value.ReferenceAsset==uuid.Nil{return zero,v2input.ErrInvalid}
 workflow,_:=v2input.WorkflowID(run)
 if value.Workflow!=workflow||value.Task!=uuid.NewSHA1(workflow,[]byte(value.Operation+":0")){return zero,v2input.ErrInvalid}
 if _,err=v2optical.DecodeMultiIntent(value.Operation,value.Intent);err!=nil{return zero,v2input.ErrInvalid}
 return value,nil
}

func(s MultiProposalService) RunReviewScope(ctx context.Context,who auth.Identity,t,run uuid.UUID)(*MultiRunReviewScope,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if s.DB==nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||t==uuid.Nil||run==uuid.Nil{return nil,v2input.ErrInvalid}
 bounded,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 var raw []byte
 err:=tenant.WithTenant(bounded,s.DB,who,t,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var role string;if err:=tx.QueryRow(bounded,"SELECT current_user").Scan(&role);err!=nil{return err};if role!="photo_api"{return v2input.ErrInvalid}
  if err:=tx.QueryRow(bounded,"SELECT photo.read_v29_multi_run_review_scope($1,$2,$3,$4)",t,run,who.UserID,who.SessionID).Scan(&raw);err!=nil{return err}
  return bounded.Err()
 })
 if err!=nil{return nil,err};if err=bounded.Err();err!=nil{return nil,err};if err=ctx.Err();err!=nil{return nil,err}
 if len(raw)==0{return nil,nil}
 value,err:=decodeMultiRunReviewScope(raw,t,run);if err!=nil{return nil,err};return &value,nil
}

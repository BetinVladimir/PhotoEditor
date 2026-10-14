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
)

type PerspectiveReplacementReviewItem struct {
 OriginalTask uuid.UUID `json:"original_task_id"`
 Project uuid.UUID `json:"project_id"`
 Scope *PerspectiveReplacementReviewScope `json:"scope"`
}
type PerspectiveReplacementReviewPage struct {
 Review uuid.UUID `json:"review_id"`
 DecisionHash string `json:"decision_hash"`
 Items []PerspectiveReplacementReviewItem `json:"items"`
 Next *uuid.UUID `json:"next_after"`
}
func decodePerspectiveReplacementReviewPage(raw []byte,tenantID,review uuid.UUID,hash string,after uuid.UUID)(PerspectiveReplacementReviewPage,error) {
 var zero,value PerspectiveReplacementReviewPage
 if len(raw)<2||len(raw)>512<<10 { return zero,v2input.ErrInvalid }
 normalized,err:=canonical.Bytes(raw);if err!=nil||json.Unmarshal(normalized,&value)!=nil { return zero,v2input.ErrInvalid }
 exact,err:=canonical.Marshal(value);if err!=nil||!bytes.Equal(normalized,exact)||value.Review!=review||value.DecisionHash!=hash||value.Items==nil||len(value.Items)>100 { return zero,v2input.ErrInvalid }
 previous:=after
 for _,item:=range value.Items {
  if item.OriginalTask==uuid.Nil||item.Project==uuid.Nil||(previous!=uuid.Nil&&item.OriginalTask.String()<=previous.String()) { return zero,v2input.ErrInvalid }
  previous=item.OriginalTask
  if item.Scope!=nil {
   body,err:=canonical.Marshal(item.Scope);if err!=nil { return zero,err }
   if _,err=decodePerspectiveReplacementReviewScope(body,tenantID,item.Project,review,item.OriginalTask,hash);err!=nil { return zero,err }
  }
 }
 if value.Next!=nil&&(len(value.Items)!=100||*value.Next!=previous) { return zero,v2input.ErrInvalid }
 return value,nil
}
func(s PerspectiveProposalService) ReplacementReviewPage(ctx context.Context,who auth.Identity,tenantID,review uuid.UUID,hash string,after uuid.UUID)(PerspectiveReplacementReviewPage,error) {
 var zero PerspectiveReplacementReviewPage
 if err:=ctx.Err();err!=nil { return zero,err }
 if s.DB==nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||tenantID==uuid.Nil||review==uuid.Nil||!validSHA256(hash) { return zero,v2input.ErrInvalid }
 bounded,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel();var raw []byte
 err:=tenant.WithTenant(bounded,s.DB,who,tenantID,domain.Editor,func(tx pgx.Tx,_ domain.Role)error {
  var role string;if err:=tx.QueryRow(bounded,"SELECT current_user").Scan(&role);err!=nil { return err };if role!="photo_api" { return v2input.ErrInvalid }
  var cursor any;if after!=uuid.Nil { cursor=after }
  return tx.QueryRow(bounded,"SELECT photo.list_v29_perspective_replacement_reviews($1,$2,$3,$4,$5,$6)",tenantID,who.UserID,who.SessionID,review,hash,cursor).Scan(&raw)
 })
 if err!=nil { return zero,err };if len(raw)==0 { return zero,v2input.ErrExpired }
 return decodePerspectiveReplacementReviewPage(raw,tenantID,review,hash,after)
}

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

type PerspectiveReplacementReviewScope struct {
 Tenant uuid.UUID `json:"tenant_id"`
 Project uuid.UUID `json:"project_id"`
 Review uuid.UUID `json:"review_id"`
 DecisionHash string `json:"decision_hash"`
 OriginalRun uuid.UUID `json:"original_run_id"`
 OriginalTask uuid.UUID `json:"original_task_id"`
 Run uuid.UUID `json:"run_id"`
 Workflow uuid.UUID `json:"workflow_id"`
 Task uuid.UUID `json:"task_id"`
 Base v2input.SessionVersionBinding `json:"base"`
}

func decodePerspectiveReplacementReviewScope(raw []byte,tenantID,project,review,original uuid.UUID,hash string)(PerspectiveReplacementReviewScope,error) {
 var zero,value PerspectiveReplacementReviewScope
 if len(raw)<2||len(raw)>4096 { return zero,v2input.ErrInvalid }
 normalized,err:=canonical.Bytes(raw);if err!=nil { return zero,v2input.ErrInvalid }
 if json.Unmarshal(normalized,&value)!=nil { return zero,v2input.ErrInvalid }
 exact,err:=canonical.Marshal(value)
 if err!=nil||!bytes.Equal(exact,normalized)||value.Tenant!=tenantID||value.Project!=project||value.Review!=review||value.OriginalTask!=original||value.DecisionHash!=hash { return zero,v2input.ErrInvalid }
 if tenantID==uuid.Nil||project==uuid.Nil||review==uuid.Nil||original==uuid.Nil||value.OriginalRun==uuid.Nil||!validLayerArtifactRef(value.Base.Source)||value.Base.AssetVersionID==uuid.Nil||!validSHA256(value.Base.CanvasHash)||!validSHA256(hash) { return zero,v2input.ErrInvalid }
 namespace:=uuid.NewSHA1(tenantID,[]byte("V29.NegativeReplacement/v2:"+project.String()+":"+review.String()+":"+hash))
 run:=uuid.NewSHA1(namespace,[]byte("run:"+value.OriginalRun.String()+":"+original.String()))
 workflow,_:=v2input.WorkflowID(run)
 if value.Run!=run||value.Workflow!=workflow||value.Task!=uuid.NewSHA1(namespace,[]byte("task:"+original.String())) { return zero,v2input.ErrInvalid }
 return value,nil
}

func(s PerspectiveProposalService) ReplacementReviewScope(ctx context.Context,who auth.Identity,tenantID,project,review,original uuid.UUID,hash string)(PerspectiveReplacementReviewScope,error) {
 var zero PerspectiveReplacementReviewScope
 if err:=ctx.Err();err!=nil { return zero,err }
 if s.DB==nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||tenantID==uuid.Nil||project==uuid.Nil||review==uuid.Nil||original==uuid.Nil||!validSHA256(hash) { return zero,v2input.ErrInvalid }
 bounded,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 var raw []byte
 err:=tenant.WithTenant(bounded,s.DB,who,tenantID,domain.Editor,func(tx pgx.Tx,_ domain.Role)error {
  var role string;if err:=tx.QueryRow(bounded,"SELECT current_user").Scan(&role);err!=nil { return err };if role!="photo_api" { return v2input.ErrInvalid }
  return tx.QueryRow(bounded,"SELECT photo.read_v29_perspective_replacement_review_scope($1,$2,$3,$4,$5,$6,$7)",tenantID,project,who.UserID,who.SessionID,review,hash,original).Scan(&raw)
 })
 if err!=nil { return zero,err };if len(raw)==0 { return zero,v2input.ErrExpired }
 return decodePerspectiveReplacementReviewScope(raw,tenantID,project,review,original,hash)
}

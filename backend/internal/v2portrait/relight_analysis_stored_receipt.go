package v2portrait

import (
 "bytes"
 "context"
 "encoding/json"
 "time"
 "github.com/google/uuid"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/v2input"
)

// Recover acknowledgement using persisted server-owned plan. SQL repeats full
// source/qualification/transport closure checks; no objects are published here.
func readStoredAnalysisReceipt(ctx context.Context, db DonorConfirmationTransactions, who auth.Identity, tenant, project uuid.UUID, key string, intent RelightAnalysisIntent) (RelightAnalysisReceipt,error) {
 var zero RelightAnalysisReceipt
 if err:=ctx.Err();err!=nil {return zero,err}
 if db==nil||tenant==uuid.Nil||project==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||!idempotency.ValidKey(key) {return zero,v2input.ErrInvalid}
 body,err:=canonical.Marshal(intent);if err!=nil{return zero,err}
 intent,err=DecodeRelightAnalysisIntent(body);if err!=nil{return zero,err}
 run:=uuid.NewSHA1(tenant,[]byte("V28.Relight.Prepare:"+project.String()+":"+who.UserID.String()+":"+intent.Operation+":"+key))
 workflow,err:=v2input.WorkflowID(run);if err!=nil{return zero,err}
 task,err:=v2input.TaskID(workflow,intent.Operation,0);if err!=nil{return zero,err}
 ctx,cancel:=context.WithTimeout(ctx,15*time.Second);defer cancel()
 tx,err:=db.Begin(ctx);if err!=nil{return zero,err};if tx==nil{return zero,v2input.ErrInvalid}
 defer tx.Rollback(context.WithoutCancel(ctx))
 if err=(relightAnalysisParamsRepository{}).authorize(ctx,tx,artifactjson.Scope{Tenant:tenant});err!=nil{return zero,err}
 var raw []byte
 if err=tx.QueryRow(ctx,"SELECT photo.read_v28_relight_analysis_stored_outcome($1,$2,$3,$4,$5,$6::jsonb)",tenant,project,run,who.UserID,who.SessionID,string(body)).Scan(&raw);err!=nil{return zero,err}
 raw,err=decodeRelightReceiptOutcome(raw);if err!=nil{return zero,err}
 var receipt RelightAnalysisReceipt
 if json.Unmarshal(raw,&receipt)!=nil{return zero,v2input.ErrInvalid}
 exact,err:=canonical.Marshal(receipt)
 if err!=nil||!bytes.Equal(raw,exact)||receipt.SchemaVersion!=1||receipt.State!="queued"||receipt.Run!=run||receipt.Workflow!=workflow||receipt.Task!=task||receipt.Actor!=who.UserID||receipt.Session!=who.SessionID||receipt.Operation!=intent.Operation||!validRelightTransportRef(receipt.Qualification) {return zero,v2input.ErrInvalid}
 if !validRelightTransportRef(v2input.Ref{ArtifactID:run,SHA256:receipt.DescriptorSHA}) {return zero,v2input.ErrInvalid}
 receipt.Transport=append(json.RawMessage(nil),receipt.Transport...)
 if err=tx.Commit(ctx);err!=nil{return zero,err}
 return receipt,nil
}

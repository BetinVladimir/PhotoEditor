package workflow

import (
 "bytes"
 "context"
 "photoeditor/backend/internal/exportpreset"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)
const watermarkPresentationCreateQuery=`SELECT photo.create_v30_watermark_presentation_task($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9)`
// CreateInTransaction reserves output and creates a blocked task only. Caller
// owns rollback/commit; no object IO or dispatch occurs in this transaction.
func(f FrozenDeliveryWatermarkPrefixTask)CreateInTransaction(ctx context.Context,tx delivery.ProofTransaction,lease delivery.ExpansionLease,request delivery.FrozenExportRequest)error{
 if err:=ctx.Err();err!=nil{return err}
 if tx==nil||lease.Tenant==uuid.Nil||lease.Tenant!=f.scope.Tenant||lease.Expansion==uuid.Nil||lease.Token==uuid.Nil||lease.Run!=request.ParentRunID||f.stage<1||f.stage>2||f.predecessor==uuid.Nil{return v2input.ErrInvalid}
 raw,err:=canonical.Marshal(request);if err!=nil{return err};if !bytes.Equal(raw,f.request){return v2input.ErrInvalid}
 draft,err:=f.DraftTask();if err!=nil{return err}
 workflow,err:=v2input.WorkflowID(request.ChildRunID);if err!=nil{return err}
 id,err:=v2input.TaskID(workflow,"derivative.watermark",0);if err!=nil||id!=draft.ID||draft.Operation!="derivative.watermark"{return v2input.ErrInvalid}
 config,err:=exportpreset.DecodeFrozenConfig([]byte(request.ConfigJCS),f.scope.PresetSHA);if err!=nil{return err}
 previous:="derivative.resize";if config.Sharpen!=nil{previous="derivative.sharpen"}
 expectedStage:=0;if config.Size!=nil{expectedStage++};if config.Sharpen!=nil{expectedStage++}
 predecessor,err:=v2input.TaskID(workflow,previous,0);if err!=nil||predecessor!=f.predecessor||f.stage!=expectedStage{return v2input.ErrInvalid}
 estimate,err:=ParseResourceEstimate(draft.ExecutionProfile);if err!=nil||estimate.OutputBytes<1{return v2input.ErrInvalid}
 task,err:=canonical.Marshal(draft);if err!=nil{return err}
 var accepted bool
 err=tx.QueryRow(ctx,watermarkPresentationCreateQuery,lease.Expansion,lease.Token,request.SlotIndex,f.stage,f.predecessor,string(task),string(f.task.closure.ParamsJCS),estimate.OutputBytes,string(f.source)).Scan(&accepted)
 if err!=nil{return err};if !accepted{return domain.NewError("DELIVERY_WATERMARK_PRESENTATION_TASK_REFUSED",409)};return ctx.Err()
}

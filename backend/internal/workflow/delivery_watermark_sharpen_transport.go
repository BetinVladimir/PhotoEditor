package workflow

import (
 "bytes"
 "context"
 "photoeditor/backend/internal/exportpreset"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)
const watermarkSharpenTransportQuery=`SELECT photo.v30_watermark_sharpen_transport_ready($1,$2,$3,$4,$5,$6::jsonb,$7)`
// SharpenTransportRegistered acknowledges only a blocked task with complete persisted
// transport under the current source/session and expansion fence.
func(f FrozenDeliveryWatermarkPrefixTask)SharpenTransportRegistered(ctx context.Context,db delivery.ProofTransaction,lease delivery.ExpansionLease,request delivery.FrozenExportRequest)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if db==nil||lease.Tenant==uuid.Nil||lease.Tenant!=f.scope.Tenant||lease.Expansion==uuid.Nil||lease.Token==uuid.Nil||lease.Run!=request.ParentRunID||f.stage<1||f.stage>2||f.predecessor==uuid.Nil{return false,v2input.ErrInvalid}
 raw,err:=canonical.Marshal(request);if err!=nil{return false,err};if !bytes.Equal(raw,f.request){return false,v2input.ErrInvalid}
 draft,err:=f.DraftTask();if err!=nil{return false,err}
 workflow,err:=v2input.WorkflowID(request.ChildRunID);if err!=nil{return false,err}
 id,err:=v2input.TaskID(workflow,"derivative.sharpen",0);if err!=nil||id!=draft.ID||draft.Operation!="derivative.sharpen"{return false,v2input.ErrInvalid}
 config,err:=exportpreset.DecodeFrozenConfig([]byte(request.ConfigJCS),f.scope.PresetSHA);if err!=nil{return false,err}
 if config.Size==nil||config.Sharpen==nil||f.stage!=1{return false,v2input.ErrInvalid}
 previous:="derivative.resize"
 predecessor,err:=v2input.TaskID(workflow,previous,0);if err!=nil||predecessor!=f.predecessor{return false,v2input.ErrInvalid}
 task,err:=canonical.Marshal(draft);if err!=nil{return false,err}
 var ready bool
 err=db.QueryRow(ctx,watermarkSharpenTransportQuery,lease.Expansion,lease.Token,request.SlotIndex,f.stage,f.predecessor,string(task),string(f.task.closure.ParamsJCS)).Scan(&ready)
 if err!=nil{return false,err};if err=ctx.Err();err!=nil{return false,err};return ready,nil
}
// RegisterSharpenInputsInTransaction attaches only the frozen closure. Caller must
// roll back every error; this does not seal the run or dispatch the task.
func(f FrozenDeliveryWatermarkPrefixTask)RegisterSharpenInputsInTransaction(ctx context.Context,tx pgx.Tx,lease delivery.ExpansionLease,request delivery.FrozenExportRequest,graph v2input.GraphTask)error{
 if err:=ctx.Err();err!=nil{return err};if tx==nil{return v2input.ErrInvalid}
 draft,err:=f.DraftTask();if err!=nil{return err};if graph.ID!=draft.ID{return v2input.ErrInvalid}
 ready,err:=f.SharpenTransportRegistered(ctx,tx,lease,request);if err!=nil||ready{return err}
 if err=f.CreateSharpenInTransaction(ctx,tx,lease,request);err!=nil{return err}
 return v2input.RegisterInTransaction(ctx,tx,lease.Tenant,request.ChildRunID,draft.ID,graph.Built,f.task.closure.Open,f.task.closure.Direct)
}

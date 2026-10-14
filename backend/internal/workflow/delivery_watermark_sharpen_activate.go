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
const watermarkSharpenActivatedQuery=`SELECT photo.v30_watermark_sharpen_activated($1,$2,$3,$4,$5,$6::jsonb,$7)`
const watermarkSharpenActivateQuery=`SELECT photo.activate_v30_watermark_sharpen($1,$2,$3,$4,$5,$6::jsonb,$7)`
// ActivateInTransaction requires complete registered transport. SQL seals and
// dispatches the successor and acknowledges the verified watermark atomically.
// Caller must roll back every error, including an ambiguous commit outcome.
func(f FrozenDeliveryWatermarkPrefixTask)ActivateSharpenInTransaction(ctx context.Context,db delivery.ProofTransaction,lease delivery.ExpansionLease,request delivery.FrozenExportRequest)error{
 if err:=ctx.Err();err!=nil{return err}
 if db==nil||lease.Tenant==uuid.Nil||lease.Tenant!=f.scope.Tenant||lease.Expansion==uuid.Nil||lease.Token==uuid.Nil||lease.Run!=request.ParentRunID||f.stage<1||f.stage>2||f.predecessor==uuid.Nil{return v2input.ErrInvalid}
 raw,err:=canonical.Marshal(request);if err!=nil{return err};if !bytes.Equal(raw,f.request){return v2input.ErrInvalid}
 draft,err:=f.DraftTask();if err!=nil{return err}
 workflow,err:=v2input.WorkflowID(request.ChildRunID);if err!=nil{return err}
 id,err:=v2input.TaskID(workflow,"derivative.sharpen",0);if err!=nil||id!=draft.ID||draft.Operation!="derivative.sharpen"{return v2input.ErrInvalid}
 config,err:=exportpreset.DecodeFrozenConfig([]byte(request.ConfigJCS),f.scope.PresetSHA);if err!=nil{return err}
 if config.Size==nil||config.Sharpen==nil||f.stage!=1{return v2input.ErrInvalid}
 previous:="derivative.resize"
 predecessor,err:=v2input.TaskID(workflow,previous,0);if err!=nil||predecessor!=f.predecessor{return v2input.ErrInvalid}
 task,err:=canonical.Marshal(draft);if err!=nil{return err}
 args:=[]any{lease.Expansion,lease.Token,request.SlotIndex,f.stage,f.predecessor,string(task),string(f.task.closure.ParamsJCS)}
 var accepted bool
 if err=db.QueryRow(ctx,watermarkSharpenActivatedQuery,args...).Scan(&accepted);err!=nil{return err};if accepted{return ctx.Err()}
 if err=db.QueryRow(ctx,watermarkSharpenActivateQuery,args...).Scan(&accepted);err!=nil{return err}
 if !accepted{return domain.NewError("DELIVERY_WATERMARK_SHARPEN_ACTIVATION_REFUSED",409)};return ctx.Err()
}

// ActivationRegistered reads the durable activation receipt under current authority.
func(f FrozenDeliveryWatermarkPrefixTask)SharpenActivationRegistered(ctx context.Context,db delivery.ProofTransaction,lease delivery.ExpansionLease,request delivery.FrozenExportRequest)(bool,error){
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
 err=db.QueryRow(ctx,watermarkSharpenActivatedQuery,lease.Expansion,lease.Token,request.SlotIndex,f.stage,f.predecessor,string(task),string(f.task.closure.ParamsJCS)).Scan(&ready)
 if err!=nil{return false,err};if err=ctx.Err();err!=nil{return false,err};return ready,nil
}

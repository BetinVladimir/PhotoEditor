package workflow
import (
 "bytes"
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)
const watermarkEncodeActivatedQuery=`SELECT photo.v30_watermark_encode_activated($1,$2,$3,$4,$5,$6::jsonb,$7)`
const watermarkEncodeActivateQuery=`SELECT photo.activate_v30_watermark_encode($1,$2,$3,$4,$5,$6::jsonb,$7)`
// ActivateInTransaction requires complete registered transport. SQL seals and
// dispatches the successor and acknowledges the verified watermark atomically.
// Caller must roll back every error, including an ambiguous commit outcome.
func(f FrozenDeliveryWatermarkEncodeTask)ActivateInTransaction(ctx context.Context,db delivery.ProofTransaction,lease delivery.ExpansionLease,request delivery.FrozenExportRequest)error{
 if err:=ctx.Err();err!=nil{return err}
 if db==nil||lease.Tenant==uuid.Nil||lease.Tenant!=f.tenant||lease.Expansion==uuid.Nil||lease.Token==uuid.Nil||lease.Run!=request.ParentRunID||f.stage<1||f.stage>3||f.predecessor==uuid.Nil{return v2input.ErrInvalid}
 raw,err:=canonical.Marshal(request);if err!=nil{return err};if !bytes.Equal(raw,f.request){return v2input.ErrInvalid}
 draft,err:=f.DraftTask();if err!=nil{return err}
 workflow,err:=v2input.WorkflowID(request.ChildRunID);if err!=nil{return err}
 id,err:=v2input.TaskID(workflow,"export.encode",0);if err!=nil||id!=draft.ID||draft.Operation!="export.encode"{return v2input.ErrInvalid}
 predecessor,err:=v2input.TaskID(workflow,"derivative.watermark",0);if err!=nil||predecessor!=f.predecessor{return v2input.ErrInvalid}
 task,err:=canonical.Marshal(draft);if err!=nil{return err}
 args:=[]any{lease.Expansion,lease.Token,request.SlotIndex,f.stage,f.predecessor,string(task),string(f.task.closure.ParamsJCS)}
 var accepted bool
 if err=db.QueryRow(ctx,watermarkEncodeActivatedQuery,args...).Scan(&accepted);err!=nil{return err};if accepted{return ctx.Err()}
 if err=db.QueryRow(ctx,watermarkEncodeActivateQuery,args...).Scan(&accepted);err!=nil{return err}
 if !accepted{return domain.NewError("DELIVERY_WATERMARK_ENCODE_ACTIVATION_REFUSED",409)};return ctx.Err()
}

// ActivationRegistered reads the durable activation receipt under current authority.
func(f FrozenDeliveryWatermarkEncodeTask)ActivationRegistered(ctx context.Context,db delivery.ProofTransaction,lease delivery.ExpansionLease,request delivery.FrozenExportRequest)(bool,error){
 if err:=ctx.Err();err!=nil{return false,err}
 if db==nil||lease.Tenant==uuid.Nil||lease.Tenant!=f.tenant||lease.Expansion==uuid.Nil||lease.Token==uuid.Nil||lease.Run!=request.ParentRunID||f.stage<1||f.stage>3||f.predecessor==uuid.Nil{return false,v2input.ErrInvalid}
 raw,err:=canonical.Marshal(request);if err!=nil{return false,err};if !bytes.Equal(raw,f.request){return false,v2input.ErrInvalid}
 draft,err:=f.DraftTask();if err!=nil{return false,err}
 workflow,err:=v2input.WorkflowID(request.ChildRunID);if err!=nil{return false,err}
 id,err:=v2input.TaskID(workflow,"export.encode",0);if err!=nil||id!=draft.ID||draft.Operation!="export.encode"{return false,v2input.ErrInvalid}
 predecessor,err:=v2input.TaskID(workflow,"derivative.watermark",0);if err!=nil||predecessor!=f.predecessor{return false,v2input.ErrInvalid}
 task,err:=canonical.Marshal(draft);if err!=nil{return false,err}
 var ready bool
 err=db.QueryRow(ctx,watermarkEncodeActivatedQuery,lease.Expansion,lease.Token,request.SlotIndex,f.stage,f.predecessor,string(task),string(f.task.closure.ParamsJCS)).Scan(&ready)
 if err!=nil{return false,err};if err=ctx.Err();err!=nil{return false,err};return ready,nil
}

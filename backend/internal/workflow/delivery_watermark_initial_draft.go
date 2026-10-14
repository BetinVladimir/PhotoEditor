package workflow

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

// WithMetadataConfig binds the current fenced parent metadata inventory.
// It does not create a task or confer activation authority.
func(f FrozenDeliveryWatermarkTask)WithMetadataConfig(ctx context.Context,store delivery.MetadataInventoryEntryStore,lease delivery.ExpansionLease,plan delivery.PreparePlan,child delivery.ExportChildIntent)(FrozenDeliveryWatermarkTask,error){
 var zero FrozenDeliveryWatermarkTask
 if err:=ctx.Err();err!=nil{return zero,err}
 request,_,err:=f.InitialRequest(lease,plan,child);if err!=nil{return zero,err}
 policy,err:=plan.MetadataPolicy();if err!=nil||policy.MetadataPolicy=="strip"{return zero,v2input.ErrInvalid}
 id:=uuid.NewSHA1(child.RunID,[]byte("v30:delivery-export:1"))
 raw,_,err:=store.CompileChild(ctx,lease.Tenant,f.scope.Project,lease.Expansion,lease.Token,id,request,policy.MetadataPolicy,policy.ApprovedFields)
 if err!=nil{return zero,err}
 f.task.metadataConfig=append([]byte(nil),raw...)
 return f,nil
}

func(f FrozenDeliveryWatermarkTask)initialChildConfig(plan delivery.PreparePlan,request delivery.FrozenExportRequest,id uuid.UUID)([]byte,error){
 policy,err:=plan.MetadataPolicy();if err!=nil{return nil,err}
 if policy.MetadataPolicy=="strip"{
  if len(f.task.metadataConfig)>0{return nil,v2input.ErrInvalid}
  return canonical.Marshal(struct{
   QueueClass string `json:"queue_class"`
   ProjectID uuid.UUID `json:"project_id"`
   Request delivery.FrozenExportRequest `json:"v30_delivery_export"`
   ExportID uuid.UUID `json:"export_id"`
  }{"batch",f.scope.Project,request,id})
 }
 var config delivery.MetadataChildConfig
 if json.Unmarshal(f.task.metadataConfig,&config)!=nil||config.ProjectID!=f.scope.Project||config.Request!=request||config.ExportID!=id||config.QueueClass!="batch"{return nil,v2input.ErrInvalid}
 raw,err:=canonical.Marshal(config)
 if err!=nil||!bytes.Equal(raw,f.task.metadataConfig){return nil,v2input.ErrInvalid}
 return raw,nil
}

// CreateDraft commits only a blocked stage-zero watermark task/export shell.
// Caller owns the transaction and must roll back on error. Transport IO belongs
// after commit; registration, activation and cursor advancement are separate.
func(f FrozenDeliveryWatermarkTask)CreateDraft(ctx context.Context,tx delivery.ProofTransaction,lease delivery.ExpansionLease,plan delivery.PreparePlan,child delivery.ExportChildIntent,exportID uuid.UUID,reservedBytes int64)error{
 if err:=ctx.Err();err!=nil{return err}
 draft,err:=f.DraftTask();if err!=nil{return err}
 if tx==nil||lease.Expansion==uuid.Nil||lease.Token==uuid.Nil||lease.Tenant==uuid.Nil||lease.Package==uuid.Nil||child.SlotIndex>lease.Cursor||exportID!=uuid.NewSHA1(child.RunID,[]byte("v30:delivery-export:1"))||reservedBytes<1||reservedBytes>9007199254740991{return v2input.ErrInvalid}
 workflow,err:=v2input.WorkflowID(lease.Run);if err!=nil||workflow!=lease.Workflow{return v2input.ErrInvalid}
 request,raw,err:=f.InitialRequest(lease,plan,child);if err!=nil{return err}
 config,err:=f.initialChildConfig(plan,request,exportID);if err!=nil{return err}
 estimate,err:=ParseResourceEstimate(draft.ExecutionProfile)
 if err!=nil||reservedBytes<estimate.OutputBytes+int64(len(config)+len(f.task.closure.ParamsJCS)){return v2input.ErrInvalid}
 task,err:=canonical.Marshal(draft);if err!=nil{return err}
 var accepted bool
 err=tx.QueryRow(ctx,`SELECT photo.create_v30_watermark_initial_child_draft($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10)`,lease.Expansion,lease.Token,child.SlotIndex,string(raw),exportID,string(config),string(task),string(f.task.closure.ParamsJCS),reservedBytes,string(f.image)).Scan(&accepted)
 if err!=nil{return err}
 if !accepted{return domain.NewError("DELIVERY_CHILD_ADMISSION_REFUSED",409)}
 return nil
}

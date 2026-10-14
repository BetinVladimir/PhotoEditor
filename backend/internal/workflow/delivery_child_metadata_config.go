package workflow

import (
 "context"
 "bytes"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)
// WithMetadataConfig binds the current leased parent inventory before draft IO.
func(f FrozenDeliveryInitialTask)WithMetadataConfig(ctx context.Context,store delivery.MetadataInventoryEntryStore,
 lease delivery.ExpansionLease,plan delivery.PreparePlan,child delivery.ExportChildIntent)(FrozenDeliveryInitialTask,error){
 var zero FrozenDeliveryInitialTask
 if _,err:=f.DraftTask();err!=nil{return zero,err}
 if lease.ParentTask!=plan.TaskID()||lease.Run==uuid.Nil{return zero,v2input.ErrInvalid}
 policy,err:=plan.MetadataPolicy();if err!=nil||policy.MetadataPolicy=="strip"{return zero,v2input.ErrInvalid}
 request,_,err:=plan.ExportRequest(lease.Run,child);if err!=nil{return zero,err}
 id,err:=plan.NewChildExportID(lease.Run,child);if err!=nil{return zero,err}
 var proof delivery.PrepareProof
 if json.Unmarshal(plan.ProofJCS(),&proof)!=nil{return zero,v2input.ErrInvalid}
 raw,_,err:=store.CompileChild(ctx,lease.Tenant,proof.ProjectID,lease.Expansion,lease.Token,id,request,policy.MetadataPolicy,policy.ApprovedFields)
 if err!=nil{return zero,err}
 f.metadataConfig=append([]byte(nil),raw...);return f,nil
}
func(f FrozenDeliveryInitialTask)childConfig(parent uuid.UUID,plan delivery.PreparePlan,child delivery.ExportChildIntent,id uuid.UUID)([]byte,string,error){
 policy,err:=plan.MetadataPolicy();if err!=nil{return nil,"",err}
 if policy.MetadataPolicy=="strip"{if len(f.metadataConfig)>0{return nil,"",v2input.ErrInvalid};return plan.ChildConfig(parent,child,id)}
 var config delivery.MetadataChildConfig
 request,_,err:=plan.ExportRequest(parent,child)
 if err!=nil||json.Unmarshal(f.metadataConfig,&config)!=nil{return nil,"",v2input.ErrInvalid}
 var proof delivery.PrepareProof
 if json.Unmarshal(plan.ProofJCS(),&proof)!=nil||config.ProjectID!=proof.ProjectID||config.ExportID!=id||config.Request!=request||config.QueueClass!="batch"{return nil,"",v2input.ErrInvalid}
 exact,err:=canonical.Marshal(config);if err!=nil||!bytes.Equal(exact,f.metadataConfig){return nil,"",v2input.ErrInvalid}
 raw:=append([]byte(nil),exact...);pin,err:=canonical.Hash(config);return raw,pin,err
}

package workflow

import (
 "github.com/google/uuid"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type DeliveryMetadataSpec struct {
 ChildRun uuid.UUID
 Encoded v2input.Ref
 EncodedJCS []byte
 FrozenJCS []byte
 FrozenSHA string
 Binding v2input.SessionVersionBinding
 Metadata v2input.Ref
 MetadataJCS []byte
 Policy string
 ApprovedFields []string
}

// CompileDeliveryMetadata creates the existing CPU metadata.write/1 node after
// canonical encode adoption. Caller must prove current parent slot/fence and
// persist the frozen pin before publication; this function does not admit work.
func CompileDeliveryMetadata(spec DeliveryMetadataSpec,registry Registry)(CompiledDownloadNode,delivery.MetadataWritePlan,error){
 var zero CompiledDownloadNode
 var empty delivery.MetadataWritePlan
 workflow,err:=v2input.WorkflowID(spec.ChildRun)
 if err!=nil{return zero,empty,err}
 task,err:=v2input.TaskID(workflow,"metadata.write",0)
 if err!=nil{return zero,empty,err}
 op,ok:=registry["metadata.write"]
 if !ok||op.Name!="metadata.write"||op.Version!="1"||op.ResourceClass!="cpu"||op.Model!=nil||op.TimeoutSeconds<1{return zero,empty,v2input.ErrInvalid}
 cpu:=false
 for _,backend:=range op.Backends{if backend=="cpu"{cpu=true}}
 if !cpu{return zero,empty,v2input.ErrInvalid}
 plan,err:=delivery.CompileMetadataWrite(task,spec.Encoded,spec.EncodedJCS,spec.FrozenJCS,spec.FrozenSHA,
 spec.Binding,spec.Metadata,spec.MetadataJCS,spec.Policy,spec.ApprovedFields)
 if err!=nil{return zero,empty,err}
 refs:=plan.Inputs()
 inputs:=make([]review.ArtifactRef,len(refs))
 for i,ref:=range refs{inputs[i]=review.ArtifactRef{ID:ref.ArtifactID,SHA256:ref.SHA256}}
 return CompiledDownloadNode{ID:task,Ordinal:0,Key:"metadata.write:0",Operation:op.Name,Version:op.Version,
 ResourceClass:op.ResourceClass,TimeoutSeconds:op.TimeoutSeconds,Inputs:inputs,Params:plan.ParamsJCS()},plan,nil
}


// PrepareDeliveryMetadataTask freezes the same Params, input digest and resource
// estimate used by blocked registration and later input publication/activation.
func PrepareDeliveryMetadataTask(spec DeliveryMetadataSpec,registry Registry,encodedBytes int64,limits DeliveryDerivativeLimits)(v2input.DraftTask,delivery.MetadataWritePlan,error){
 var zero v2input.DraftTask
 var empty delivery.MetadataWritePlan
 node,plan,err:=CompileDeliveryMetadata(spec,registry)
 if err!=nil{return zero,empty,err}
 profile,err:=CompileDeliveryMetadataProfile(node,spec.EncodedJCS,encodedBytes,limits)
 if err!=nil{return zero,empty,err}
 closure,err:=plan.InputClosure(spec.ChildRun)
 if err!=nil{return zero,empty,err}
 return v2input.DraftTask{ID:node.ID,NodeKey:node.Key,Operation:node.Operation,OperationVersion:node.Version,
 ResourceClass:node.ResourceClass,ParamsArtifactID:plan.Params().ArtifactID,ParamsSHA:plan.Params().SHA256,
 InputDigest:closure.InputDigest,ExecutionProfile:profile},plan,nil
}

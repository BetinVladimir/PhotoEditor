package workflow

import (
 "context"
 "fmt"
 "github.com/google/uuid"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

// PublishedDeliveryMetadataSpec contains references from frozen child admission,
// not caller-authored normalized metadata or a self-selected frozen projection.
// EncodedBytes must be current immutable raster evidence, not a client estimate.
type PublishedDeliveryMetadataSpec struct {
 ParentRun,ChildRun uuid.UUID
 SlotIndex int
 Encoded v2input.Ref
 EncodedJCS []byte
 EncodedBytes int64
 Binding v2input.SessionVersionBinding
 Metadata v2input.Ref
 Policy string
 ApprovedFields []string
}
func(p PublishedDeliveryMetadataSpec)validateLease(lease delivery.MetadataBarrierLease)error{
 if p.ParentRun==uuid.Nil||p.ChildRun==uuid.Nil||p.SlotIndex<0||p.SlotIndex>=80000||
 p.ChildRun!=uuid.NewSHA1(p.ParentRun,[]byte(fmt.Sprintf("child:export:%d",p.SlotIndex)))||
 lease.ChildRun!=p.ChildRun||lease.Tenant==uuid.Nil||lease.Expansion==uuid.Nil||lease.Token==uuid.Nil||lease.Until.IsZero(){return v2input.ErrInvalid}
 workflow,err:=v2input.WorkflowID(p.ChildRun);if err!=nil{return err}
 predecessor,err:=v2input.TaskID(workflow,"export.encode",0)
 if err!=nil||predecessor!=lease.ParentTask{return v2input.ErrInvalid}
 if p.EncodedBytes<1||p.EncodedBytes>2*1024*1024*1024{return v2input.ErrInvalid}
 return nil
}

// StagePublished resolves source evidence in a short scheduler SQL transaction
// before invoking the existing fenced materialization/registration coordinator.
// The caller still discovers work and supplies current frozen child admission.
func(s DeliveryMetadataStage)StagePublished(ctx context.Context,lease delivery.MetadataBarrierLease,request PublishedDeliveryMetadataSpec)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil||s.Objects==nil||len(s.Bucket)<3||len(s.Bucket)>63{return v2input.ErrInvalid}
 if err:=request.validateLease(lease);err!=nil{return err}
 request.EncodedJCS=append([]byte(nil),request.EncodedJCS...)
 request.ApprovedFields=append([]string{},request.ApprovedFields...)
 sources,err:=delivery.NewMetadataSourceStore(s.DB,lease.Tenant);if err!=nil{return err}
 publication,err:=sources.Load(ctx,delivery.MetadataSourceReadScope{Tenant:lease.Tenant,ParentRun:request.ParentRun,
 Binding:request.Binding,Metadata:request.Metadata,Policy:request.Policy,ApprovedFields:request.ApprovedFields})
 if err!=nil{return err}
 spec,err:=request.withPublication(publication);if err!=nil{return err}
 if err=ctx.Err();err!=nil{return err}
 return s.Stage(ctx,lease,spec,request.EncodedBytes)
}
func(p PublishedDeliveryMetadataSpec)withPublication(publication delivery.MetadataSourcePublication)(DeliveryMetadataSpec,error){
 var zero DeliveryMetadataSpec
 frozen,source:=publication.FrozenJCS(),publication.SourceJCS()
 pin:=renderDerivativeBytesSHA(frozen)
 if publication.Ref()!=p.Metadata{return zero,v2input.ErrInvalid}
 if _,err:=delivery.RestoreDeliveryMetadata(frozen,pin,p.Binding,p.Metadata,source,p.Policy,p.ApprovedFields);err!=nil{return zero,err}
 return DeliveryMetadataSpec{ChildRun:p.ChildRun,Encoded:p.Encoded,EncodedJCS:append([]byte(nil),p.EncodedJCS...),
 FrozenJCS:frozen,FrozenSHA:pin,Binding:p.Binding,Metadata:p.Metadata,MetadataJCS:source,
 Policy:p.Policy,ApprovedFields:append([]string{},p.ApprovedFields...)},nil
}

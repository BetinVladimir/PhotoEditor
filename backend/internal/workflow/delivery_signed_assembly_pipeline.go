package workflow

import (
 "context"

 "github.com/google/uuid"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

// RegisterAndActivateSignedAssembly binds a published complete V31 closure to
// the single blocked Delivery task, then applies the physical signed-object
// barrier before the fenced parent transition. Every stage is replayable by
// immutable refs; no object IO is performed inside the final transaction.
func(p DeliveryAssemblyPipeline)RegisterAndActivateSignedAssembly(ctx context.Context,
 lease delivery.ExpansionLease,plan delivery.PreparePlan,
 publication delivery.SignedAssemblyPublication,actor uuid.UUID,
 slots []delivery.Slot,dependencies []delivery.SigningDependency,
 receipts []delivery.SignedDependencyReceipt,resolved []v2input.Entry)error{
 if err:=ctx.Err();err!=nil{return err}
 if p.DB==nil||p.Objects==nil||p.SignedObjects==nil||!lease.Valid()||
  lease.Cursor!=lease.SlotCount||lease.ParentTask!=plan.TaskID()||
  actor==uuid.Nil||len(slots)!=lease.SlotCount||
  len(dependencies)==0||len(dependencies)!=len(receipts)||
  len(p.Bucket)<3||len(p.Bucket)>63{return v2input.ErrInvalid}
 // Pure closure/resource preflight must precede any params publication.
 // An incomplete required bundle must not create durable transport documents.
 profile,err:=CompileSignedDeliveryAssemblyProfile(ctx,publication,lease,
  actor,slots,dependencies,receipts,resolved,p.Limits)
 if err!=nil{return err}
 params,_,err:=PublishSignedDeliveryAssemblyParams(ctx,
  deliveryAssemblyPublicationQueries{db:p.DB,lease:lease},p.Objects,p.Bucket,
  lease,plan,publication.EnvelopeRef)
 if err!=nil{return err}
 frozen,err:=FreezePublishedSignedDeliveryAssemblyTask(lease,plan,publication,
  params,actor,slots,dependencies,receipts,resolved,profile)
 if err!=nil{return err}
 if _,err=p.RegisterPublishedSignedAssembly(ctx,lease,plan,frozen);err!=nil{return err}
 return p.ActivatePublishedSignedAssembly(ctx,lease,plan,frozen,publication)
}

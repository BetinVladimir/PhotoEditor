package delivery
import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)
// EncodeWatermarkSelectionPending freezes candidate documents before source
// version selection. Its stage is distinct from executable pending evidence.
// A caller must recheck catalogue/selection before persistence.
func(i CreateIntent)EncodeWatermarkSelectionPending(ctx context.Context,session uuid.UUID,catalog CreateCatalog,items []v2input.SessionSnapshotItem)([]byte,error){
 docs,err:=i.projectCreateDocuments(ctx,session,catalog,items);if err!=nil{return nil,err}
 raw,err:=canonical.Marshal(CreatePending{Stage:"selection",QueueClass:"batch",Project:i.request.ProjectID,SchemaVersion:1,Tenant:i.tenant,Actor:i.actor,RequestSHA:i.requestSHA,Catalog:catalog,Items:items,Documents:pendingCreateDocuments(docs)})
 if err!=nil{return nil,err};if len(raw)>createPendingMaxBytes{return nil,domain.NewError("DELIVERY_PENDING_TOO_LARGE",422)};return raw,ctx.Err()
}
// DecodeWatermarkSelectionPending validates only frozen selection values. It
// cannot replace DecodeWatermarkPending/SQL pin authority at package admission.
func(i CreateIntent)DecodeWatermarkSelectionPending(ctx context.Context,session uuid.UUID,raw []byte)(CreatePending,error){return i.decodePendingProjection(ctx,session,raw,nil,false,true)}

// Own the validated immutable selection before any asynchronous SQL/object IO.
// Returning canonical owned bytes also keeps all later decoders on one payload.
func(i CreateIntent)ownWatermarkSelectionPending(ctx context.Context,session uuid.UUID,raw []byte)([]byte,error){
 pending,err:=i.DecodeWatermarkSelectionPending(ctx,session,raw)
 if err!=nil{return nil,err}
 owned,err:=canonical.Marshal(pending)
 if err!=nil{return nil,err}
 return owned,ctx.Err()
}

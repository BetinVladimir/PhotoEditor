package delivery

import (
 "context"
 "errors"
 "fmt"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)

type SigningChildStatus struct {
 State string
 ExportID uuid.UUID
 TaskID uuid.UUID
}

// ReadDeliverySignerStatus observes only the exact leased parent slot. Even
// `adopted` must pass current V31 policy/receipt and physical byte checks.
func ReadDeliverySignerStatus(ctx context.Context,db ProofTransaction,lease ExpansionLease,
 actor uuid.UUID,dependency SigningDependency)(SigningChildStatus,error){
 if err:=ctx.Err();err!=nil{return SigningChildStatus{},err}
 expectedKey:=uuid.NewSHA1(lease.Run,[]byte(fmt.Sprintf("%d:%s",dependency.Ordinal,dependency.PresetID))).String()
 if db==nil||!lease.valid()||lease.Cursor!=lease.SlotCount||actor==uuid.Nil||
  dependency.Ordinal<0||dependency.Ordinal>4999||dependency.PresetID==uuid.Nil||dependency.ExportID==uuid.Nil||
  dependency.IdempotencyKey!=expectedKey{return SigningChildStatus{},v2input.ErrInvalid}
 var status SigningChildStatus
 err:=db.QueryRow(ctx,`SELECT signing_status,export_id,task_id FROM photo.read_v30_delivery_signer_status($1,$2,$3,$4)`,
  lease.Expansion,lease.Token,dependency.Ordinal,dependency.PresetID).Scan(&status.State,&status.ExportID,&status.TaskID)
 if errors.Is(err,pgx.ErrNoRows){return SigningChildStatus{},domain.NewError("DELIVERY_SIGNER_STATUS_STALE",409)}
 if err!=nil{return SigningChildStatus{},err}
 workflow:=uuid.NewSHA1(lease.Tenant,[]byte("provenance:"+actor.String()+":"+expectedKey))
 expectedTask:=uuid.NewSHA1(workflow,[]byte("provenance.sign:0"))
 if status.ExportID!=dependency.ExportID||status.TaskID!=expectedTask{return SigningChildStatus{},v2input.ErrInvalid}
 switch status.State{
 case "not_started","pending","failed","cancelled","succeeded_unverified","adopted":
 default:return SigningChildStatus{},v2input.ErrInvalid
 }
 return status,ctx.Err()
}

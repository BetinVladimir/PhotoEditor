package workflow

import (
 "context"
 "strconv"
 "strings"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

// Dedicated assembly transport cannot publish documents for another task/run.
type deliveryAssemblyTransportQueries struct{db delivery.MetadataTransactionBeginner;lease delivery.ExpansionLease;task uuid.UUID}
type deliveryAssemblyTransportRow struct{owner deliveryAssemblyTransportQueries;ctx context.Context;query string;args []any}
func(q deliveryAssemblyTransportQueries)QueryRow(ctx context.Context,query string,args ...any)pgx.Row{return deliveryAssemblyTransportRow{q,ctx,query,append([]any(nil),args...)}}
func(r deliveryAssemblyTransportRow)Scan(dest ...any)error{
 if err:=r.ctx.Err();err!=nil{return err};q:=r.owner
 if q.db==nil||!q.lease.Valid(){return v2input.ErrInvalid}
 expected,err:=v2input.TaskID(q.lease.Workflow,"v2.delivery.assemble",0)
 if err!=nil||expected!=q.task{return v2input.ErrInvalid}
 switch r.query{
 case "SELECT outcome,artifact_id,token,ready FROM photo.reserve_v2_input_document($1,$2,$3,$4,$5,$6,$7)":
  if len(r.args)!=7{return v2input.ErrInvalid}
 case "SELECT photo.complete_v2_input_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)":
  if len(r.args)!=10{return v2input.ErrInvalid}
 default:return v2input.ErrInvalid
 }
 tenant,ok:=r.args[0].(uuid.UUID);run,good:=r.args[1].(uuid.UUID);task,taskOK:=r.args[2].(uuid.UUID);purpose,purposeOK:=r.args[3].(string)
 if !ok||!good||!taskOK||!purposeOK||tenant!=q.lease.Tenant||run!=q.lease.Run||task!=q.task{return v2input.ErrInvalid}
 if purpose!="v2_input_manifest"{
  suffix,found:=strings.CutPrefix(purpose,"v2_input_chunk_")
  ordinal,err:=strconv.Atoi(suffix)
  if !found||err!=nil||ordinal<0||ordinal>1953||strconv.Itoa(ordinal)!=suffix{return v2input.ErrInvalid}
 }
 return assemblyTenantTransaction(r.ctx,q.db,q.lease,func(ctx context.Context,tx pgx.Tx)error{return tx.QueryRow(ctx,r.query,r.args...).Scan(dest...)})
}

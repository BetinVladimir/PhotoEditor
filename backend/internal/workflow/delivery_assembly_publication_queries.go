package workflow

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)

// Reserve and complete each commit independently, on either side of object IO.
type deliveryAssemblyPublicationQueries struct{db delivery.MetadataTransactionBeginner;lease delivery.ExpansionLease}
type deliveryAssemblyPublicationRow struct{owner deliveryAssemblyPublicationQueries;ctx context.Context;query string;args []any}
func(q deliveryAssemblyPublicationQueries)QueryRow(ctx context.Context,query string,args ...any)pgx.Row{
 return deliveryAssemblyPublicationRow{q,ctx,query,append([]any(nil),args...)}
}
func(r deliveryAssemblyPublicationRow)Scan(dest ...any)error{
 if err:=r.ctx.Err();err!=nil{return err}
 q:=r.owner;l:=q.lease
 if q.db==nil||!l.Valid()||l.Cursor!=l.SlotCount{return v2input.ErrInvalid}
 count,scope:=0,-1
 switch r.query{
 case `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_delivery_shard($1,$2,$3,$4,$5,$6,$7,$8)`,
      `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_delivery_assembly($1,$2,$3,$4::jsonb,$5,$6,$7,$8)`,
      `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_delivery_signed_document($1,$2,$3,$4,$5,$6,$7,$8)`:
  count,scope=8,5
 case `SELECT photo.complete_v30_delivery_shard($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
      `SELECT photo.complete_v30_delivery_assembly($1,$2,$3,$4::jsonb,$5,$6,$7,$8,$9,$10,$11)`,
      `SELECT photo.complete_v30_delivery_signed_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`:
  count,scope=11,8
 case `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_delivery_assembly_params($1,$2,$3,$4,$5,$6,$7)`:
  count,scope=7,4
 case `SELECT photo.complete_v30_delivery_assembly_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`:
  count,scope=10,7
 case `SELECT photo.retain_v30_delivery_assembly($1,$2,$3,$4)`:
  count=4
 default:return v2input.ErrInvalid
 }
 if len(r.args)!=count{return v2input.ErrInvalid}
 expansion,ok:=r.args[0].(uuid.UUID);token,good:=r.args[1].(uuid.UUID)
 if !ok||!good||expansion!=l.Expansion||token!=l.Token{return v2input.ErrInvalid}
 if scope>=0{
  tenant,ok:=r.args[scope].(uuid.UUID);run,good:=r.args[scope+1].(uuid.UUID);owner,ownerOK:=r.args[scope+2].(uuid.UUID)
  if !ok||!good||!ownerOK||tenant!=l.Tenant||run!=l.Run||owner==uuid.Nil{return v2input.ErrInvalid}
 }else{
  root,ok:=r.args[2].(uuid.UUID);sha,good:=r.args[3].(string)
  if !ok||!good||root==uuid.Nil||domainDigestInvalid(sha){return v2input.ErrInvalid}
 }
 return assemblyTenantTransaction(r.ctx,q.db,l,func(ctx context.Context,tx pgx.Tx)error{return tx.QueryRow(ctx,r.query,r.args...).Scan(dest...)})
}

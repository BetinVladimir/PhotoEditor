package delivery

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
// PinMetadataInventory runs only after complete per-version publication and
// creation documents. SQL rechecks the live idempotency/source authority.
func(i CreateIntent)PinMetadataInventory(ctx context.Context,db ProofTransaction,session uuid.UUID,
 inventory FrozenMetadataInventory,snapshot []byte)error{
 if err:=ctx.Err();err!=nil{return err}
 if db==nil||i.identity.Run!=inventory.value.ParentRun||i.tenant==uuid.Nil||i.actor==uuid.Nil||session==uuid.Nil||inventory.pin==""||
 digest(inventory.body)!=inventory.pin||digest(snapshot)!=inventory.value.Snapshot.SHA256||i.request.Policy.MetadataPolicy!=inventory.value.Policy{return v2input.ErrInvalid}
 fields,err:=normalizeMetadataApproval(i.request.Policy.MetadataPolicy,i.request.Policy.ApprovedFields);if err!=nil{return err}
 if len(fields)!=len(inventory.value.ApprovedFields){return v2input.ErrInvalid}
 for n,field:=range fields{if field!=inventory.value.ApprovedFields[n]{return v2input.ErrInvalid}}
 var pinned bool
 err=db.QueryRow(ctx,`SELECT photo.pin_v30_metadata_inventory($1,$2,$3,$4,$5,$6,$7,$8,$9)`,i.tenant,i.identity.Run,i.actor,session,string(inventory.body),inventory.pin,string(snapshot),i.key,i.requestSHA).Scan(&pinned)
 if err!=nil{return err};if !pinned{return v2input.ErrExpired};return ctx.Err()
}

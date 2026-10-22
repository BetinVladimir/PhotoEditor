package collection

import (
 "bytes"
 "context"
 "encoding/json"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/tenant"
)

// BulkAbandonRequest carries the original immutable selection and both original
// keys. A missing snapshot response is recovered under the Selection.Create lock.
type BulkAbandonRequest struct {
 SelectionKey string `json:"selection_key"`
 Selection SelectionRequest `json:"selection"`
 Action string `json:"action"`
 Params json.RawMessage `json:"params"`
}
type bulkAbandoned struct { Status string `json:"status"`; Key string `json:"key"`; CollectionID uuid.UUID `json:"collection_id"` }
func bulkAbandonBody(id uuid.UUID,key string)([]byte,error){return canonical.Marshal(bulkAbandoned{"abandoned",key,id})}
func isBulkAbandoned(out idempotency.Response,id uuid.UUID,key string)bool{
 body,e:=bulkAbandonBody(id,key);return e==nil&&out.Status==200&&out.ResourceID==nil&&bytes.Equal(body,out.Body)
}
func bindBulkAbandoned(ctx context.Context,tx pgx.Tx,who auth.Identity,tid,id uuid.UUID,route,key string,out idempotency.Response)error{
 if !isBulkAbandoned(out,id,key){return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
 // Legacy unbound responses cannot prove their parent merely by matching JSON.
 if err:=idempotency.BindParent(ctx,tx,idempotency.Scope{Tenant:tid,Principal:who.UserID,PrincipalType:"user",Route:route,Key:key},out,"collection:"+id.String(),nil);err!=nil{return err}
 tag,err:=tx.Exec(ctx,`UPDATE photo.idempotency_records SET expires_at='9999-01-01T00:00:00Z'::timestamptz WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route=$3 AND key=$4 AND response_status=200 AND resource_id IS NULL`,tid,who.UserID,route,key)
 if err!=nil{return err};if tag.RowsAffected()!=1{return domain.NewError("IDEMPOTENCY_CONFLICT",409)};return nil
}

// Lock order is Selection receipt -> Bulk receipt -> domain proof reads. Normal
// admission never takes these receipts in reverse order. No fake snapshot exists:
// nil is ONLY a tombstone fingerprint which conflicts with every valid late start.
func(s Service)AbandonBulk(ctx context.Context,who auth.Identity,tid,id uuid.UUID,key string,raw BulkAbandonRequest)(idempotency.Response,error){
 var out idempotency.Response
 selection,revision,err:=normalizeSelection(raw.Selection);if err!=nil{return out,err}
 if !idempotency.ValidKey(raw.SelectionKey)||!idempotency.ValidKey(key){return out,domain.NewError("VALIDATION_FAILED",422)}
 req,target,err:=normalizeBulk(BulkRequest{ExpectedRevision:revision,SelectionSnapshotID:uuid.MustParse("00000000-0000-4000-8000-000000000001"),Action:raw.Action,Params:raw.Params});if err!=nil{return out,err}
 if target!=nil&&*target==id{return out,domain.NewError("VALIDATION_FAILED",422)}
 forever:=time.Date(9999,1,1,0,0,0,0,time.UTC)
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  authority:=func()error{r,e:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor);if e!=nil{return e};return currentEditorProject(ctx,tx,who,tid,id,r)}
  if e:=authority();e!=nil{return e}
  tombstone:=func()(idempotency.Response,error){if e:=authority();e!=nil{return idempotency.Response{},e};body,e:=bulkAbandonBody(id,raw.SelectionKey);return idempotency.Response{Status:200,Body:body},e}
  selected,e:=idempotency.Use(ctx,tx,idempotency.Scope{Tenant:tid,Principal:who.UserID,PrincipalType:"user",Route:"Selection.Create",Key:raw.SelectionKey},selection,forever,tombstone);if e!=nil{return e}
  var snapshotRef SnapshotRef
  if selected.Status==201{
   if e=bindCollectionReceipt(ctx,tx,who,tid,id,"Selection.Create",raw.SelectionKey,selected,`SELECT EXISTS(SELECT 1 FROM photo.snapshots WHERE tenant_id=$1 AND id=$2 AND collection_id=$3 AND kind='selection' AND principal_type='user')`);e!=nil{return e}
   var ref SnapshotRef;if e=json.Unmarshal(selected.Body,&ref);e!=nil{return e};if selected.ResourceID==nil||ref.ID!=*selected.ResourceID||ref.ID==uuid.Nil{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
   var principalOK bool;if e=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM photo.snapshots WHERE tenant_id=$1 AND id=$2 AND principal_type='user' AND principal_id=$3)`,tid,ref.ID,who.UserID).Scan(&principalOK);e!=nil{return e};if !principalOK{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
   canonicalRef,e:=canonical.Marshal(ref);if e!=nil||!bytes.Equal(canonicalRef,selected.Body)||len(ref.Digest)!=64||ref.ItemCount<1||ref.ItemCount>5000||ref.ExpiresAt.IsZero(){return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
   var exact bool;if e=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM photo.snapshots WHERE tenant_id=$1 AND id=$2 AND collection_id=$3 AND source_revision=$4 AND digest=$5 AND item_count=$6 AND expires_at=$7 AND kind='selection' AND principal_type='user' AND principal_id=$8)`,tid,ref.ID,id,revision,ref.Digest,ref.ItemCount,ref.ExpiresAt,who.UserID).Scan(&exact);e!=nil{return e};if !exact{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
   if selection.Scope=="selected"&&ref.ItemCount!=int64(len(selection.ItemIDs)){return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
   snapshotRef=ref
   req.SelectionSnapshotID=ref.ID
  }else{
   if e=bindBulkAbandoned(ctx,tx,who,tid,id,"Selection.Create",raw.SelectionKey,selected);e!=nil{return e};req.SelectionSnapshotID=uuid.Nil
  }
  out,e=idempotency.Use(ctx,tx,idempotency.Scope{Tenant:tid,Principal:who.UserID,PrincipalType:"user",Route:"Bulk.Start",Key:key},req,forever,func()(idempotency.Response,error){if e:=authority();e!=nil{return idempotency.Response{},e};body,e:=bulkAbandonBody(id,key);return idempotency.Response{Status:200,Body:body},e});if e!=nil{return e}
  if out.Status==202{
   if e=bindCollectionReceipt(ctx,tx,who,tid,id,"Bulk.Start",key,out,`SELECT EXISTS(SELECT 1 FROM photo.bulk_operations WHERE tenant_id=$1 AND id=$2 AND collection_id=$3)`);e!=nil{return e}
   var accepted BulkAccepted;if e=json.Unmarshal(out.Body,&accepted);e!=nil{return e};encoded,e:=canonical.Marshal(accepted);if e!=nil||!bytes.Equal(encoded,out.Body)||out.ResourceID==nil||accepted.BulkOperationID!=*out.ResourceID||accepted.RunID==uuid.Nil||accepted.Status!="queued"{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
   var exact bool;if e=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM photo.bulk_operations WHERE tenant_id=$1 AND id=$2 AND collection_id=$3 AND snapshot_id=$4 AND action=$5 AND params=$6::jsonb AND run_id=$7 AND created_by=$8)`,tid,accepted.BulkOperationID,id,req.SelectionSnapshotID,req.Action,req.Params,accepted.RunID,who.UserID).Scan(&exact);e!=nil{return e};if !exact{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
   resolved,e:=canonical.Marshal(struct{Status string `json:"status"`; Snapshot SnapshotRef `json:"snapshot"`; Ticket BulkAccepted `json:"ticket"`}{"accepted",snapshotRef,accepted});if e!=nil{return e};out.Body=resolved
   if target!=nil{r,e:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor);if e!=nil{return e};if e=currentEditorProject(ctx,tx,who,tid,*target,r);e!=nil{return e}}
  }else if e=bindBulkAbandoned(ctx,tx,who,tid,id,"Bulk.Start",key,out);e!=nil{return e}
  if e=authority();e!=nil{return e};return ctx.Err()
 });return out,err
}

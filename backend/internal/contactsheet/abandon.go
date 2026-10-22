package contactsheet

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/tenant"
)

type Abandoned struct { Status string `json:"status"`; Key string `json:"key"`; SelectionSnapshotID uuid.UUID `json:"selection_snapshot_id"` }
func abandonedBody(request CreateRequest,key string)([]byte,error){return canonical.Marshal(Abandoned{"abandoned",key,request.SelectionSnapshotID})}
func isAbandoned(out idempotency.Response,request CreateRequest,key string)bool{body,err:=abandonedBody(request,key);return err==nil&&out.Status==200&&out.ResourceID==nil&&bytes.Equal(body,out.Body)}
func receiptScope(who auth.Identity,tid uuid.UUID,key string)idempotency.Scope{return idempotency.Scope{Tenant:tid,Principal:who.UserID,PrincipalType:"user",Route:"ContactSheet.Create",Key:key}}
func authorizeSelection(ctx context.Context,tx pgx.Tx,who auth.Identity,tid uuid.UUID,request CreateRequest)error{
 var projectID uuid.UUID
 err:=tx.QueryRow(ctx,`SELECT c.project_id FROM photo.snapshots s JOIN photo.collections c ON c.tenant_id=s.tenant_id AND c.id=s.collection_id AND c.deleted_at IS NULL WHERE s.tenant_id=$1 AND s.id=$2 AND s.kind='selection' AND s.principal_type='user' AND s.principal_id=$3`,tid,request.SelectionSnapshotID,who.UserID).Scan(&projectID)
 if errors.Is(err,pgx.ErrNoRows){return domain.NewError("NOT_FOUND",404)};if err!=nil{return err}
 role,err:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor);if err!=nil{return err}
 _,err=project.Authorize(ctx,tx,tid,projectID,who.UserID,role,domain.Editor,false);return err
}
// Validate the stored acceptance independently of parent_binding: its presence
// proves receipt scope, not the integrity of an arbitrary response/resource tuple.
func validateReceipt(ctx context.Context,tx pgx.Tx,who auth.Identity,tid uuid.UUID,key string,request CreateRequest,out idempotency.Response)error{
 parent:="selection:"+request.SelectionSnapshotID.String()
 if isAbandoned(out,request,key){return idempotency.BindParent(ctx,tx,receiptScope(who,tid,key),out,parent,nil)}
 var accepted Accepted
 if out.Status!=202||out.ResourceID==nil||*out.ResourceID==uuid.Nil||json.Unmarshal(out.Body,&accepted)!=nil||accepted.RunID!=*out.ResourceID||accepted.Status!="queued"{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
 canonicalBody,err:=canonical.Marshal(accepted);if err!=nil{return err};if !bytes.Equal(canonicalBody,out.Body){return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
 proof:=func()error{
  var valid bool
  err:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM photo.runs WHERE tenant_id=$1 AND id=$2 AND kind='export' AND input_snapshot_id=$3 AND created_by=$4
   AND config_snapshot->>'output_kind'='contact_sheet' AND config_snapshot->>'selection_snapshot_id'=$3::text
   AND config_snapshot->>'locale'=$5 AND config_snapshot->'columns'=to_jsonb($6::int) AND config_snapshot->'rows'=to_jsonb($7::int))`,tid,accepted.RunID,request.SelectionSnapshotID,who.UserID,request.Locale,request.Columns,request.Rows).Scan(&valid)
  if err!=nil{return err};if !valid{return domain.NewError("IDEMPOTENCY_CONFLICT",409)};return nil
 }
 if err=proof();err!=nil{return err};return idempotency.BindParent(ctx,tx,receiptScope(who,tid,key),out,parent,proof)
}

// Abandon uses the original normalized request and receipt key. It cannot
// delete/cancel an accepted run, allocate resources, or admit a replacement.
func(s Service)Abandon(ctx context.Context,who auth.Identity,tid uuid.UUID,key string,raw CreateRequest)(idempotency.Response,error){
 var out idempotency.Response
 if err:=ctx.Err();err!=nil{return out,err}
 request,err:=normalize(raw);if err!=nil{return out,err};if !idempotency.ValidKey(key){return out,domain.NewError("IDEMPOTENCY_KEY_REQUIRED",400)}
 if s.DB==nil{return out,domain.NewError("DEPENDENCY_UNAVAILABLE",503)}
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  if err:=authorizeSelection(ctx,tx,who,tid,request);err!=nil{return err}
  var err error
  out,err=idempotency.Use(ctx,tx,receiptScope(who,tid,key),request,time.Date(9999,1,1,0,0,0,0,time.UTC),func()(idempotency.Response,error){
   if err:=authorizeSelection(ctx,tx,who,tid,request);err!=nil{return idempotency.Response{},err};body,err:=abandonedBody(request,key);return idempotency.Response{Status:200,Body:body},err
  });if err!=nil{return err}
  if err=validateReceipt(ctx,tx,who,tid,key,request,out);err!=nil{return err}
  if isAbandoned(out,request,key){
   tag,e:=tx.Exec(ctx,`UPDATE photo.idempotency_records SET expires_at='9999-01-01T00:00:00Z'::timestamptz WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route='ContactSheet.Create' AND key=$3 AND response_status=200 AND resource_id IS NULL`,tid,who.UserID,key)
   if e!=nil{return e};if tag.RowsAffected()!=1{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
  }
  if err=authorizeSelection(ctx,tx,who,tid,request);err!=nil{return err};return ctx.Err()
 });return out,err
}

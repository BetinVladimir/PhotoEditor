package run

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
type SaveEditsAbandoned struct{Status string `json:"status"`;Key string `json:"key"`;CollectionID uuid.UUID `json:"collection_id"`}
func saveEditsAbandonBody(collection uuid.UUID,key string)([]byte,error){return canonical.Marshal(SaveEditsAbandoned{"abandoned",key,collection})}
func isSaveEditsAbandoned(out idempotency.Response,collection uuid.UUID,key string)bool{body,e:=saveEditsAbandonBody(collection,key);return e==nil&&out.Status==200&&out.ResourceID==nil&&bytes.Equal(body,out.Body)}
func saveEditsApplyRequest(req SaveEditsRequest)(ApplyRequest,bool,error){
 if req.OverridesID==uuid.Nil||req.StyleVersionID!=nil&&*req.StyleVersionID==uuid.Nil{return ApplyRequest{},false,domain.NewError("VALIDATION_FAILED",422)}
 style:=uuid.Nil;if req.StyleVersionID!=nil{style=*req.StyleVersionID}
 normalized,_,e:=normalizeApply(ApplyRequest{SourceRevision:req.SourceRevision,StyleVersionID:style,Scope:req.Scope,ItemIDs:append([]uuid.UUID(nil),req.SelectedItemIDs...),OverridesID:&req.OverridesID,OutputName:req.OutputName})
 return normalized,req.StyleVersionID==nil,e
}
func bindSaveEditsReceipt(ctx context.Context,tx pgx.Tx,who auth.Identity,tid,collection uuid.UUID,key string,out idempotency.Response)error{
 scope:=idempotency.Scope{Tenant:tid,Principal:who.UserID,PrincipalType:"user",Route:"Run.SaveEdits",Key:key}
 if isSaveEditsAbandoned(out,collection,key){return idempotency.BindParent(ctx,tx,scope,out,"collection:"+collection.String(),func()error{return nil})}
 var ticket ApplyAccepted
 if out.Status!=202||out.ResourceID==nil||*out.ResourceID==uuid.Nil||json.Unmarshal(out.Body,&ticket)!=nil||ticket.RunID!=*out.ResourceID||ticket.OutputCollectionID==uuid.Nil||ticket.Status!="queued"{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
 encoded,e:=canonical.Marshal(ticket);if e!=nil||!bytes.Equal(encoded,out.Body){return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
 prove:=func()error{var valid bool;e:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT FROM photo.runs r JOIN photo.snapshots s ON s.tenant_id=r.tenant_id AND s.id=r.input_snapshot_id WHERE r.tenant_id=$1 AND r.id=$2 AND r.created_by=$3 AND r.output_collection_id=$4 AND s.collection_id=$5 AND r.kind='apply')`,tid,ticket.RunID,who.UserID,ticket.OutputCollectionID,collection).Scan(&valid);if e!=nil{return e};if !valid{return domain.NewError("IDEMPOTENCY_CONFLICT",409)};return nil}
 if e=prove();e!=nil{return e};return idempotency.BindParent(ctx,tx,scope,out,"collection:"+collection.String(),prove)
}
// ResolveSaveEdits abandons only the original unaccepted command under its
// original receipt lock. A committed 202 always wins; no run is cancelled.
func(s Service)ResolveSaveEdits(ctx context.Context,who auth.Identity,tid,collection uuid.UUID,key string,raw SaveEditsRequest)(out idempotency.Response,err error){
 req,_,e:=saveEditsApplyRequest(raw);if e!=nil{return out,e};if collection==uuid.Nil{return out,domain.NewError("VALIDATION_FAILED",422)}
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  var projectID uuid.UUID;e:=tx.QueryRow(ctx,`SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`,tid,collection).Scan(&projectID);if errors.Is(e,pgx.ErrNoRows){return domain.NewError("NOT_FOUND",404)};if e!=nil{return e}
  authorize:=func()error{current,e:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor);if e!=nil{return e};_,e=project.Authorize(ctx,tx,tid,projectID,who.UserID,current,domain.Editor,false);return e};if e=authorize();e!=nil{return e}
  scope:=idempotency.Scope{Tenant:tid,Principal:who.UserID,PrincipalType:"user",Route:"Run.SaveEdits",Key:key}
  fingerprint:=struct{Source uuid.UUID `json:"source_collection_id"`;Request ApplyRequest `json:"request"`}{collection,req}
  out,e=idempotency.Use(ctx,tx,scope,fingerprint,time.Date(9999,1,1,0,0,0,0,time.UTC),func()(idempotency.Response,error){if e:=authorize();e!=nil{return idempotency.Response{},e};body,e:=saveEditsAbandonBody(collection,key);return idempotency.Response{Status:200,Body:body},e});if e!=nil{return e}
  if e=bindSaveEditsReceipt(ctx,tx,who,tid,collection,key,out);e!=nil{return e}
  if isSaveEditsAbandoned(out,collection,key){tag,e:=tx.Exec(ctx,`UPDATE photo.idempotency_records SET expires_at='9999-01-01T00:00:00Z'::timestamptz WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route='Run.SaveEdits' AND key=$3 AND response_status=200 AND resource_id IS NULL`,tid,who.UserID,key);if e!=nil{return e};if tag.RowsAffected()!=1{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}}
  var currentProject uuid.UUID
  if e=tx.QueryRow(ctx,`SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL FOR SHARE`,tid,collection).Scan(&currentProject);errors.Is(e,pgx.ErrNoRows){return domain.NewError("NOT_FOUND",404)}else if e!=nil{return e};if currentProject!=projectID{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
  if e=authorize();e!=nil{return e};return ctx.Err()
 });return out,err
}

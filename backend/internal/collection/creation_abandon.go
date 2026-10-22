package collection

import (
 "bytes"
 "context"
 "time"
 "strings"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/project"
 "photoeditor/backend/internal/tenant"
)

// CreationAbandoned is a permanent receipt, never a deletion command.
type CreationAbandoned struct {
 Status string `json:"status"`
 Key string `json:"key"`
 CollectionID *uuid.UUID `json:"collection_id,omitempty"`
 ProjectID *uuid.UUID `json:"project_id,omitempty"`
}
func creationAbandonBody(route,key string,parent uuid.UUID)([]byte,error){
 value:=CreationAbandoned{Status:"abandoned",Key:key}
 if route=="Share.Create"{value.CollectionID=&parent}else{value.ProjectID=&parent}
 return canonical.Marshal(value)
}
func isCreationAbandoned(out idempotency.Response,route,key string,parent uuid.UUID)bool{
 expected,err:=creationAbandonBody(route,key,parent)
 return err==nil&&out.Status==200&&out.ResourceID==nil&&bytes.Equal(expected,out.Body)
}
func sealCreationAbandon(ctx context.Context,tx pgx.Tx,who auth.Identity,tid uuid.UUID,route,key string,parent uuid.UUID,out idempotency.Response)error{
 if !isCreationAbandoned(out,route,key,parent){return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
 prefix:="project:";if route=="Share.Create"{prefix="collection:"}
 if err:=idempotency.BindParent(ctx,tx,idempotency.Scope{Tenant:tid,Principal:who.UserID,PrincipalType:"user",Route:route,Key:key},out,prefix+parent.String(),func()error{return nil});err!=nil{return err}
 tag,err:=tx.Exec(ctx,`UPDATE photo.idempotency_records SET expires_at='9999-01-01T00:00:00Z'::timestamptz WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route=$3 AND key=$4 AND response_status=200 AND resource_id IS NULL`,tid,who.UserID,route,key)
 if err!=nil{return err};if tag.RowsAffected()!=1{return domain.NewError("IDEMPOTENCY_CONFLICT",409)};return nil
}
func frozenCollectionCreate(req Create)Create{
 if req.InitialShare!=nil{
  copied:=*req.InitialShare
  if copied.Limits.AllowedFormats!=nil{copied.Limits.AllowedFormats=append([]string{},copied.Limits.AllowedFormats...)}
  if copied.AllowedPresets!=nil{copied.AllowedPresets=append([]uuid.UUID{},copied.AllowedPresets...)}
  if copied.ExpiresAt.Value!=nil{value:=*copied.ExpiresAt.Value;copied.ExpiresAt.Value=&value}
  req.InitialShare=&copied
 };return req
}
func validateFrozenShareReceipt(value ShareCreated,collectionID uuid.UUID,expected shareCreateIntent)error{
 conflict:=func()error{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
 if value.ID==uuid.Nil||value.CollectionID!=collectionID||value.PolicyVersion!="1"||value.CreatedAt.IsZero()||value.RevokedAt!=nil||value.Secret!=""{return conflict()}
 var permissions SharePermissions;var limits ShareLimits;var presets []uuid.UUID
 if strictShareJSON(value.Permissions,&permissions)!=nil||strictShareJSON(value.Limits,&limits)!=nil||strictShareJSON(value.AllowedPresets,&presets)!=nil{return conflict()}
 observed,err:=freezeShareIntent(collectionID,ShareCreate{Label:value.Label,Permissions:permissions,Limits:limits,Moderation:value.Moderation,AllowedPresets:presets,ExpiresAt:OptionalTime{Set:true,Value:value.ExpiresAt}});if err!=nil{return conflict()}
 if expected.Expiry.Mode=="omitted"{if value.ExpiresAt==nil{return conflict()};observed.Expiry=expected.Expiry}else if expected.Expiry.Mode=="null"{if value.ExpiresAt!=nil{return conflict()};observed.Expiry=expected.Expiry}
 // pgx encodes timestamptz in whole microseconds (Nanosecond()/1000).
 // Keep the original nanosecond intent/fingerprint, but compare its exact
 // persisted timestamp projection rather than rejecting valid fresh receipts.
 if expected.Expiry.Mode=="explicit"{if expected.Expiry.Value==nil||value.ExpiresAt==nil||!value.ExpiresAt.Equal(expected.Expiry.Value.UTC().Truncate(time.Microsecond)){return conflict()};observed.Expiry=expected.Expiry}
 a,err:=canonical.Hash(expected);if err!=nil{return err};b,err:=canonical.Hash(observed);if err!=nil{return err};if a!=b{return conflict()};return nil
}
func(s Service)AbandonShareCreation(ctx context.Context,who auth.Identity,tid,collectionID uuid.UUID,key string,req ShareCreate)(idempotency.Response,error){
 var out idempotency.Response
 intent,err:=freezeShareIntent(collectionID,req);if err!=nil{return out,err}
 if !idempotency.ValidKey(key){return out,domain.NewError("IDEMPOTENCY_KEY_REQUIRED",400)}
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  if e:=currentEditorProject(ctx,tx,who,tid,collectionID,role);e!=nil{return e}
  request,e:=shareRequestForReceipt(ctx,tx,who,tid,key,intent);if e!=nil{return e}
  out,e=idempotency.Use(ctx,tx,idempotency.Scope{Tenant:tid,Principal:who.UserID,PrincipalType:"user",Route:"Share.Create",Key:key},request,time.Date(9999,1,1,0,0,0,0,time.UTC),func()(idempotency.Response,error){
   current,e:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor);if e!=nil{return idempotency.Response{},e};if e=currentEditorProject(ctx,tx,who,tid,collectionID,current);e!=nil{return idempotency.Response{},e}
   body,e:=creationAbandonBody("Share.Create",key,collectionID);return idempotency.Response{Status:200,Body:body},e
  });if e!=nil{return e}
  if out.Status==201{
   var value ShareCreated
   if out.ResourceID==nil||strictShareJSON(out.Body,&value)!=nil||value.Secret!=""||value.ID!=*out.ResourceID||value.CollectionID!=collectionID||value.PolicyVersion!="1"||value.CreatedAt.IsZero(){return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
   if e=validateFrozenShareReceipt(value,collectionID,intent);e!=nil{return e}
   if e=bindCollectionReceipt(ctx,tx,who,tid,collectionID,"Share.Create",key,out,`SELECT EXISTS(SELECT 1 FROM photo.share_links WHERE tenant_id=$1 AND id=$2 AND collection_id=$3)`);e!=nil{return e}
  }else if e=sealCreationAbandon(ctx,tx,who,tid,"Share.Create",key,collectionID,out);e!=nil{return e}
  current,e:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor);if e!=nil{return e};if e=currentEditorProject(ctx,tx,who,tid,collectionID,current);e!=nil{return e};return ctx.Err()
 });return out,err
}
func(s Service)AbandonCreation(ctx context.Context,who auth.Identity,tid uuid.UUID,key string,req Create)(idempotency.Response,error){
 var out idempotency.Response
 if req.ProjectID==uuid.Nil||!valid(req.Name,req.Description,"manual","draft"){return out,domain.NewError("VALIDATION_FAILED",422)}
 if !idempotency.ValidKey(key){return out,domain.NewError("IDEMPOTENCY_KEY_REQUIRED",400)}
 req=frozenCollectionCreate(req)
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  if _,e:=project.Authorize(ctx,tx,tid,req.ProjectID,who.UserID,role,domain.Editor,false);e!=nil{return e}
  var e error
  out,e=idempotency.Use(ctx,tx,idempotency.Scope{Tenant:tid,Principal:who.UserID,PrincipalType:"user",Route:"Collection.Create",Key:key},req,time.Date(9999,1,1,0,0,0,0,time.UTC),func()(idempotency.Response,error){
   current,e:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor);if e!=nil{return idempotency.Response{},e};if _,e=project.Authorize(ctx,tx,tid,req.ProjectID,who.UserID,current,domain.Editor,false);e!=nil{return idempotency.Response{},e}
   body,e:=creationAbandonBody("Collection.Create",key,req.ProjectID);return idempotency.Response{Status:200,Body:body},e
  });if e!=nil{return e}
  if out.Status==201{
   var value CollectionCreated
   if out.ResourceID==nil||strictShareJSON(out.Body,&value)!=nil||value.Collection.ID!=*out.ResourceID||value.Collection.TenantID!=tid||value.Collection.ProjectID!=req.ProjectID||value.Collection.Name!=strings.TrimSpace(req.Name)||value.Collection.Description!=req.Description||value.Collection.CreatedAt.IsZero()||(value.InitialShare!=nil&&value.InitialShare.Secret!=""){return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
   if (value.InitialShare!=nil)!=(req.InitialShare!=nil){return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
   if req.InitialShare==nil {if value.Collection.State!="draft"||value.Collection.Revision!="1"{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}}else if value.Collection.State!="published"||value.Collection.Revision!="2"{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
   if value.InitialShare!=nil{
    share:=value.InitialShare
    if share.ID==uuid.Nil||share.CollectionID!=value.Collection.ID||share.PolicyVersion!="1"||share.CreatedAt.IsZero()||share.RevokedAt!=nil{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
    // The effective default timestamp is taken only from this immutable
    // accepted receipt; it is never recomputed from the present clock.
    expected,e:=freezeShareIntent(value.Collection.ID,*req.InitialShare);if e!=nil{return e}
    if e=validateFrozenShareReceipt(*share,value.Collection.ID,expected);e!=nil{return e}
    var bound bool;if e=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM photo.share_links WHERE tenant_id=$1 AND id=$2 AND collection_id=$3)`,tid,share.ID,value.Collection.ID).Scan(&bound);e!=nil{return e};if !bound{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
   }
   e=idempotency.BindParent(ctx,tx,idempotency.Scope{Tenant:tid,Principal:who.UserID,PrincipalType:"user",Route:"Collection.Create",Key:key},out,"project:"+req.ProjectID.String(),func()error{var exists bool;if e:=tx.QueryRow(ctx,`SELECT EXISTS(SELECT 1 FROM photo.collections WHERE tenant_id=$1 AND id=$2 AND project_id=$3)`,tid,*out.ResourceID,req.ProjectID).Scan(&exists);e!=nil{return e};if !exists{return domain.NewError("IDEMPOTENCY_CONFLICT",409)};return nil});if e!=nil{return e}
  }else if e=sealCreationAbandon(ctx,tx,who,tid,"Collection.Create",key,req.ProjectID,out);e!=nil{return e}
  current,e:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor);if e!=nil{return e};if _,e=project.Authorize(ctx,tx,tid,req.ProjectID,who.UserID,current,domain.Editor,false);e!=nil{return e};return ctx.Err()
 });return out,err
}

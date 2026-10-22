package collection

import (
 "bytes"
 "context"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/tenant"
)

type SectionAbandoned struct {
 Status string `json:"status"`
 Key string `json:"key"`
 CollectionID uuid.UUID `json:"collection_id"`
}
func sectionAbandonBody(collectionID uuid.UUID,key string)([]byte,error){return canonical.Marshal(SectionAbandoned{Status:"abandoned",Key:key,CollectionID:collectionID})}
func isSectionAbandoned(out idempotency.Response,collectionID uuid.UUID,key string)bool{
 if out.Status!=200||out.ResourceID!=nil{return false};expected,err:=sectionAbandonBody(collectionID,key);return err==nil&&bytes.Equal(expected,out.Body)
}
func bindSectionAbandoned(ctx context.Context,tx pgx.Tx,who auth.Identity,tid,collectionID uuid.UUID,key string,out idempotency.Response)error{
 if !isSectionAbandoned(out,collectionID,key){return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
 return idempotency.BindParent(ctx,tx,idempotency.Scope{Tenant:tid,Principal:who.UserID,PrincipalType:"user",Route:"Collection.CreateSection",Key:key},out,"collection:"+collectionID.String(),func()error{return nil})
}
// AbandonSectionCreation serializes against CreateSection under its ORIGINAL
// receipt identity. It neither deletes a section nor cancels an accepted create.
func(s Service)AbandonSectionCreation(ctx context.Context,who auth.Identity,tid,collectionID uuid.UUID,key string,req SectionCreate)(idempotency.Response,error){
 var out idempotency.Response
 if req.ExpectedRevision<1||!validSection(req.Name,req.Position){return out,domain.NewError("VALIDATION_FAILED",422)}
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,role domain.Role)error{
  if err:=currentEditorProject(ctx,tx,who,tid,collectionID,role);err!=nil{return err}
  var err error
  out,err=idempotency.Use(ctx,tx,idempotency.Scope{Tenant:tid,Principal:who.UserID,PrincipalType:"user",Route:"Collection.CreateSection",Key:key},req,time.Date(9999,1,1,0,0,0,0,time.UTC),func()(idempotency.Response,error){
   currentRole,e:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor);if e!=nil{return idempotency.Response{},e}
   if e=currentEditorProject(ctx,tx,who,tid,collectionID,currentRole);e!=nil{return idempotency.Response{},e}
   body,e:=sectionAbandonBody(collectionID,key);return idempotency.Response{Status:200,Body:body},e
  });if err!=nil{return err}
  if out.Status==201{if err=bindCollectionReceipt(ctx,tx,who,tid,collectionID,"Collection.CreateSection",key,out,`SELECT EXISTS(SELECT 1 FROM photo.sections WHERE tenant_id=$1 AND id=$2 AND collection_id=$3)`);err!=nil{return err}}else{
   if err=bindSectionAbandoned(ctx,tx,who,tid,collectionID,key,out);err!=nil{return err}
   // Use's ON CONFLICT deliberately retains the old expiry. An existing
   // unsealed receipt must acquire the same permanent tombstone retention.
   tag,e:=tx.Exec(ctx,`UPDATE photo.idempotency_records SET expires_at='9999-01-01T00:00:00Z'::timestamptz WHERE tenant_id=$1 AND principal_type='user' AND principal_id=$2 AND route='Collection.CreateSection' AND key=$3 AND response_status=200 AND resource_id IS NULL`,tid,who.UserID,key);if e!=nil{return e};if tag.RowsAffected()!=1{return domain.NewError("IDEMPOTENCY_CONFLICT",409)}
  }
  currentRole,e:=tenant.AuthorizeInTransaction(ctx,tx,who,tid,domain.Editor);if e!=nil{return e};if e=currentEditorProject(ctx,tx,who,tid,collectionID,currentRole);e!=nil{return e};return ctx.Err()
 });return out,err
}

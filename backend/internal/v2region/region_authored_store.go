package v2region

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/database"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/v2input"
)

// AuthoredRegionRecord owns request intent, never child success. State is derived
// from real child canonical/adoption/resource facts at every status read.
type AuthoredRegionRecord struct {
 ID uuid.UUID
 Intent AuthoredRegionIntent
 SHA string
 RenderBatch, CropRun uuid.UUID
 CancelRequested bool
 Revision int64
 Current bool
}

// validate binds loaded children to this request's immutable content and
// cancellation domain. A different run with the same recipe is not its child.
func(r AuthoredRegionRecord)validate()error{
 if r.ID==uuid.Nil||r.Revision<1||r.Revision>9007199254740991{return ErrRegionInvalid}
 hash,err:=r.Intent.SHA256();if err!=nil||hash!=r.SHA{return ErrRegionInvalid}
 renderKey,err:=r.Intent.RequestChildKey(r.ID,"render");if err!=nil{return err}
 cropKey,err:=r.Intent.RequestChildKey(r.ID,"crop");if err!=nil{return err}
 render,err:=v2input.RunID(r.Intent.Tenant,r.Intent.Actor,renderKey);if err!=nil{return err}
 crop,err:=v2input.RunID(r.Intent.Tenant,r.Intent.Actor,cropKey);if err!=nil{return err}
 if r.RenderBatch!=render||r.CropRun!=crop||r.ID==render||r.ID==crop||render==crop{return ErrRegionInvalid}
 return nil
}

type AuthoredRegionStore interface {
 Begin(context.Context, AuthoredRegionIntent, string) (AuthoredRegionRecord,error)
 Read(context.Context, auth.Identity, uuid.UUID,uuid.UUID,uuid.UUID) (AuthoredRegionRecord,error)
 Cancel(context.Context, auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID) error
}

type SQLAuthoredRegionStore struct { API,Scheduler *pgxpool.Pool }

func authoredRegionIDs(i AuthoredRegionIntent,key string)(uuid.UUID,uuid.UUID,uuid.UUID,error){
 if _,err:=i.SHA256();err!=nil{return uuid.Nil,uuid.Nil,uuid.Nil,err}
 // Request IDs have their own domain. They are not RunID/JobTicket claims.
 if !idempotency.ValidKey(key){return uuid.Nil,uuid.Nil,uuid.Nil,ErrRegionInvalid}
 id:=uuid.NewSHA1(i.Tenant,[]byte("AuthoredRegion.Admit:"+i.Actor.String()+":"+key))
 renderKey,err:=i.RequestChildKey(id,"render");if err!=nil{return uuid.Nil,uuid.Nil,uuid.Nil,err}
 cropKey,err:=i.RequestChildKey(id,"crop");if err!=nil{return uuid.Nil,uuid.Nil,uuid.Nil,err}
 render,err:=v2input.RunID(i.Tenant,i.Actor,renderKey);if err!=nil{return uuid.Nil,uuid.Nil,uuid.Nil,err}
 crop,err:=v2input.RunID(i.Tenant,i.Actor,cropKey);return id,render,crop,err
}

func requireAuthoredPoolRole(ctx context.Context,pool *pgxpool.Pool,role string)error{
 if pool==nil{return ErrRegionUnavailable};var actual string
 if err:=pool.QueryRow(ctx,"SELECT current_user").Scan(&actual);err!=nil{return err}
 if actual!=role{return ErrRegionInvalid};return nil
}

func(s SQLAuthoredRegionStore) Begin(ctx context.Context,i AuthoredRegionIntent,key string)(AuthoredRegionRecord,error){
 var empty AuthoredRegionRecord
 if err:=requireAuthoredPoolRole(ctx,s.Scheduler,"photo_scheduler");err!=nil{return empty,err}
 id,render,crop,err:=authoredRegionIDs(i,key);if err!=nil{return empty,err}
 raw,err:=canonical.Marshal(i);if err!=nil{return empty,err}
 hash,err:=i.SHA256();if err!=nil{return empty,err}
 var outcome string
 if err=s.Scheduler.QueryRow(ctx,"SELECT photo.begin_authored_region($1,$2,$3,$4,$5,$6,$7)",i.Tenant,id,key,string(raw),hash,render,crop).Scan(&outcome);err!=nil{return empty,err}
 switch outcome {case "created","replayed":case "conflict":return empty,v2input.ErrConflict;case "stale","cancelled":return empty,v2input.ErrExpired;default:return empty,ErrRegionInvalid}
 // Never return a fabricated revision/current flag from an INSERT outcome.
 return s.read(ctx,s.Scheduler,"photo_scheduler",auth.Identity{UserID:i.Actor,SessionID:i.Session},i.Tenant,i.Asset,id)
}
func(s SQLAuthoredRegionStore)Read(ctx context.Context,who auth.Identity,tenant,asset,id uuid.UUID)(AuthoredRegionRecord,error){
 return s.read(ctx,s.API,"photo_api",who,tenant,asset,id)
}
func(s SQLAuthoredRegionStore)read(ctx context.Context,pool *pgxpool.Pool,role string,who auth.Identity,tenant,asset,id uuid.UUID)(AuthoredRegionRecord,error){
 var out AuthoredRegionRecord
 if tenant==uuid.Nil||asset==uuid.Nil||id==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil{return out,ErrRegionInvalid}
 if err:=requireAuthoredPoolRole(ctx,pool,role);err!=nil{return out,err}
 var raw []byte
 err:=pool.QueryRow(ctx,"SELECT intent_jcs,intent_sha,render_batch_id,crop_run_id,cancel_requested,revision,is_current FROM photo.read_authored_region($1,$2,$3,$4,$5)",tenant,id,asset,who.UserID,who.SessionID).Scan(&raw,&out.SHA,&out.RenderBatch,&out.CropRun,&out.CancelRequested,&out.Revision,&out.Current)
 if errors.Is(err,pgx.ErrNoRows){return out,v2input.ErrExpired};if err!=nil{return out,err}
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(raw,normalized)||json.Unmarshal(raw,&out.Intent)!=nil{return AuthoredRegionRecord{},ErrRegionInvalid}
 hash,err:=out.Intent.SHA256();if err!=nil||hash!=out.SHA||out.Intent.Tenant!=tenant||out.Intent.Asset!=asset||out.Intent.Actor!=who.UserID||out.Intent.Session!=who.SessionID||out.Revision<1{return AuthoredRegionRecord{},ErrRegionInvalid}
 renderKey,_:=out.Intent.RequestChildKey(id,"render");cropKey,_:=out.Intent.RequestChildKey(id,"crop")
 render,_:=v2input.RunID(tenant,who.UserID,renderKey);crop,_:=v2input.RunID(tenant,who.UserID,cropKey)
 if render!=out.RenderBatch||crop!=out.CropRun{return AuthoredRegionRecord{},ErrRegionInvalid}
 out.ID=id;if err:=out.validate();err!=nil{return AuthoredRegionRecord{},err};return out,nil
}
func(s SQLAuthoredRegionStore)Cancel(ctx context.Context,who auth.Identity,tenant,asset,id uuid.UUID)error{
 if err:=requireAuthoredPoolRole(ctx,s.API,"photo_api");err!=nil{return err}
 return database.TenantTransaction(ctx,s.API,tenant,func(tx pgx.Tx)error{
  var ok bool
  if err:=tx.QueryRow(ctx,"SELECT photo.cancel_authored_region($1,$2,$3,$4,$5)",tenant,id,asset,who.UserID,who.SessionID).Scan(&ok);err!=nil{return err}
  if !ok{return v2input.ErrExpired};return nil
 })
}

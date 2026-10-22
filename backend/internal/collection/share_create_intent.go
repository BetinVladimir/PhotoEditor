package collection

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "io"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
)

// New hashes bind the original expiry intent. A generated default timestamp is
// an outcome, never part of the identity of an omitted-expiry request.
type shareExpiryIntent struct { Mode string `json:"mode"`; Value *time.Time `json:"value"` }
type shareCreateIntent struct {
 SchemaVersion int `json:"schema_version"`
 Collection uuid.UUID `json:"collection_id"`
 Label string `json:"label"`
 Expiry shareExpiryIntent `json:"expiry_intent"`
 Permissions SharePermissions `json:"permissions"`
 Limits ShareLimits `json:"limits"`
 Moderation string `json:"moderation"`
 Presets []uuid.UUID `json:"allowed_presets"`
}
type legacyShareCreateRequest struct {
 Collection uuid.UUID `json:"collection_id"`
 Label string `json:"label"`
 Expires *time.Time `json:"expires_at"`
 Permissions SharePermissions `json:"permissions"`
 Limits ShareLimits `json:"limits"`
 Moderation string `json:"moderation"`
 Presets []uuid.UUID `json:"allowed_presets"`
}

func freezeShareIntent(collection uuid.UUID, req ShareCreate) (shareCreateIntent,error) {
 limits:=req.Limits;limits.AllowedFormats=append([]string(nil),req.Limits.AllowedFormats...)
 // Validate timeless fields before lookup. Temporal validity belongs only to
 // fresh admission, so a committed expired share can still be recovered.
 label,limits,moderation,presets,err:=normalizeShare(req.Label,req.Permissions,limits,req.Moderation,append([]uuid.UUID(nil),req.AllowedPresets...),nil,time.Time{})
 if err!=nil{return shareCreateIntent{},err}
 expiry:=shareExpiryIntent{Mode:"omitted"}
 if req.ExpiresAt.Set {expiry.Mode="null";if req.ExpiresAt.Value!=nil{value:=req.ExpiresAt.Value.UTC();expiry=shareExpiryIntent{Mode:"explicit",Value:&value}}}
 return shareCreateIntent{2,collection,label,expiry,req.Permissions,limits,moderation,presets},nil
}

func (intent shareCreateIntent) request() ShareCreate {
 limits:=intent.Limits;limits.AllowedFormats=append([]string(nil),intent.Limits.AllowedFormats...)
 expiry:=OptionalTime{Set:intent.Expiry.Mode!="omitted"}
 if intent.Expiry.Value!=nil { value:=*intent.Expiry.Value;expiry.Value=&value }
 return ShareCreate{Label:intent.Label,ExpiresAt:expiry,Permissions:intent.Permissions,Limits:limits,Moderation:intent.Moderation,AllowedPresets:append([]uuid.UUID(nil),intent.Presets...)}
}

func strictShareJSON(raw []byte, out any)error{
 decoder:=json.NewDecoder(bytes.NewReader(raw));decoder.DisallowUnknownFields()
 if err:=decoder.Decode(out);err!=nil{return err};var extra any
 if err:=decoder.Decode(&extra);err!=io.EOF{return errors.New("share receipt has trailing JSON")};return nil
}

// Select only a proven old fingerprint. The historical response is immutable
// evidence; a mutable live share row cannot replace its expiry or permissions.
func shareRequestForReceipt(ctx context.Context,tx pgx.Tx,who auth.Identity,tid uuid.UUID,key string,intent shareCreateIntent)(any,error){
 var hash string;var status *int;var body []byte;var resource *uuid.UUID
 err:=tx.QueryRow(ctx,`SELECT request_hash,response_status,response_body,resource_id
 FROM photo.idempotency_records WHERE tenant_id=$1 AND principal_type='user'
 AND principal_id=$2 AND route='Share.Create' AND key=$3 FOR UPDATE`,tid,who.UserID,key).Scan(&hash,&status,&body,&resource)
 if errors.Is(err,pgx.ErrNoRows){return intent,nil};if err!=nil{return nil,err}
 if err=ctx.Err();err!=nil{return nil,err}
 if err=idempotency.ValidateReceiptAuthority(ctx,tx);err!=nil{return nil,err}
 currentHash,err:=canonical.Hash(intent);if err!=nil{return nil,err};if hash==currentHash{return intent,nil}
 conflict:=func()(any,error){return nil,domain.NewError("IDEMPOTENCY_CONFLICT",409)}
 if status==nil||*status!=201||resource==nil||*resource==uuid.Nil||len(body)<2||len(body)>65536{return conflict()}
 var frozen ShareCreated
 if strictShareJSON(body,&frozen)!=nil||frozen.Secret!=""||frozen.ID!=*resource||frozen.CollectionID!=intent.Collection||frozen.PolicyVersion!="1"||frozen.CreatedAt.IsZero()||frozen.RevokedAt!=nil{return conflict()}
 var permissions SharePermissions;var limits ShareLimits;var presets []uuid.UUID
 if strictShareJSON(frozen.Permissions,&permissions)!=nil||strictShareJSON(frozen.Limits,&limits)!=nil||strictShareJSON(frozen.AllowedPresets,&presets)!=nil{return conflict()}
 legacy:=legacyShareCreateRequest{frozen.CollectionID,frozen.Label,frozen.ExpiresAt,permissions,limits,frozen.Moderation,presets}
 proof,err:=canonical.Hash(legacy);if err!=nil||proof!=hash{return conflict()}
 candidate:=legacyShareCreateRequest{intent.Collection,intent.Label,intent.Expiry.Value,intent.Permissions,intent.Limits,intent.Moderation,intent.Presets}
 if intent.Expiry.Mode=="omitted" {
  // Old receipts never recorded whether a nonnull effective expiry was a
  // default or explicit input. Prove all other fields, then fail closed rather
  // than infer original intent from a timestamp tolerance or rewrite a hash.
  candidate.Expires=frozen.ExpiresAt
  proof,err=canonical.Hash(candidate);if err!=nil||proof!=hash{return conflict()}
  return nil,domain.NewError("SHARE_REPLAY_INTENT_REQUIRED",409)
 }
 proof,err=canonical.Hash(candidate);if err!=nil||proof!=hash{return conflict()}
 return candidate,nil
}

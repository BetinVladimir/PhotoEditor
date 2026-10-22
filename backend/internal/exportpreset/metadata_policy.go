package exportpreset

import (
 "context"
 "encoding/json"
 "errors"
 "strconv"
 "time"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/idempotency"
 "photoeditor/backend/internal/tenant"
)

type MetadataPolicyChange struct {
 Revision string `json:"revision"`
 MetadataPolicy string `json:"metadata_policy"`
}
func validateMetadataPolicyChange(req MetadataPolicyChange)(int64,error){
 revision,err:=strconv.ParseInt(req.Revision,10,64)
 if err!=nil||revision<1||revision>=9007199254740991||strconv.FormatInt(revision,10)!=req.Revision||
 (req.MetadataPolicy!="strip"&&req.MetadataPolicy!="copyright"&&req.MetadataPolicy!="approved_exif"){
  return 0,domain.NewError("VALIDATION_FAILED",422)
 }
 return revision,nil
}
// ChangeMetadataPolicy preserves every other rendering parameter. Frozen
// delivery configs remain immutable; subsequent selections see the new hash.
func(s Service)ChangeMetadataPolicy(ctx context.Context,who auth.Identity,tid,id uuid.UUID,key string,req MetadataPolicyChange)(idempotency.Response,error){
 var out idempotency.Response
 revision,err:=validateMetadataPolicyChange(req);if err!=nil{return out,err}
 if id==uuid.Nil{return out,domain.NewError("VALIDATION_FAILED",422)}
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var inner error
  scope:=struct{ID uuid.UUID `json:"preset_id"`;Change MetadataPolicyChange `json:"change"`}{id,req}
  out,inner=idempotency.Use(ctx,tx,idempotency.Scope{Tenant:tid,Principal:who.UserID,PrincipalType:"user",Route:"ExportPreset.MetadataPolicy",Key:key},scope,time.Now().Add(24*time.Hour),func()(idempotency.Response,error){
   var preset Preset;var current int64;var raw []byte
   err:=tx.QueryRow(ctx,`SELECT id,name,revision,config,config_hash,created_at FROM photo.export_presets WHERE tenant_id=$1 AND id=$2 AND archived_at IS NULL FOR UPDATE`,tid,id).Scan(&preset.ID,&preset.Name,&current,&raw,&preset.ConfigHash,&preset.CreatedAt)
   if err==pgx.ErrNoRows{return idempotency.Response{},domain.NewError("NOT_FOUND",404)};if err!=nil{return idempotency.Response{},err}
   if current!=revision{return idempotency.Response{},domain.NewError("REVISION_CONFLICT",409)}
   body,hash,config,err:=compileMetadataPolicyConfig(raw,req.MetadataPolicy)
   if err!=nil{return idempotency.Response{},err}
   preset.Config=config
   _,err=tx.Exec(ctx,`UPDATE photo.export_presets SET config=$3::jsonb,config_hash=$4,revision=revision+1 WHERE tenant_id=$1 AND id=$2`,tid,id,string(body),hash)
   if err!=nil{return idempotency.Response{},err}
   preset.Revision=strconv.FormatInt(current+1,10);preset.ConfigHash=hash
   response,err:=canonical.Marshal(preset)
   return idempotency.Response{Status:200,Body:response,ResourceID:&preset.ID},err
  });if inner!=nil{return inner}
  receipt,receiptErr:=readPresetReceipt(out,200);if receiptErr!=nil{return receiptErr}
  if receipt.ID!=id||receipt.Revision!=strconv.FormatInt(revision+1,10)||receipt.Config.MetadataPolicy!=req.MetadataPolicy{return errors.New("export preset metadata receipt binding mismatch")}
  return nil
 });return out,err
}

func compileMetadataPolicyConfig(raw []byte,policy string)([]byte,string,Config,error){
 var zero Config
 if _,err:=validateMetadataPolicyChange(MetadataPolicyChange{"1",policy});err!=nil{return nil,"",zero,err}
 var config map[string]json.RawMessage
 if json.Unmarshal(raw,&config)!=nil||config==nil{return nil,"",zero,domain.NewError("INTERNAL_ERROR",500)}
 encoded,_:=json.Marshal(policy);config["metadata_policy"]=encoded
 body,err:=canonical.Marshal(config);if err!=nil{return nil,"",zero,err}
 hash,err:=canonical.Hash(config);if err!=nil{return nil,"",zero,err}
 if err=json.Unmarshal(body,&zero);err!=nil{return nil,"",Config{},err}
 if err=ValidateConfig(zero);err!=nil{return nil,"",Config{},err}
 return body,hash,zero,nil
}

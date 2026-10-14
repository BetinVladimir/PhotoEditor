package delivery

import (
 "context"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/exportpreset"
 "photoeditor/backend/internal/v2input"
)
const watermarkPinRetryQuery=`SELECT outcome,pin_jcs,pin_sha FROM photo.read_v30_watermark_pin_retry($1,$2,$3,$4,$5,$6,$7,$8,$9)`
// ReadWatermarkPinRetry uses the caller's API tenant transaction. SQL repeats
// live source authorization for saved pins. No rows means expired authority,
// never absence; a saved pin must not be replaced with a newly selected version.
func(i CreateIntent)ReadWatermarkPinRetry(ctx context.Context,db ProofTransaction,currentSession uuid.UUID,scope WatermarkPinScope,configRaw []byte)(FrozenWatermarkSource,bool,error){
 var zero FrozenWatermarkSource
 if err:=ctx.Err();err!=nil{return zero,false,err}
 if db==nil||currentSession==uuid.Nil||i.key==""||i.tenant==uuid.Nil||i.actor==uuid.Nil||i.identity.Run==uuid.Nil||scope.Tenant!=i.tenant||scope.Actor!=i.actor||scope.Parent!=i.identity.Run||scope.Project!=i.request.ProjectID{return zero,false,v2input.ErrInvalid}
 selected:=false
 for _,id:=range i.request.Policy.RequiredPresets{if id==scope.Preset{selected=true}}
 for _,id:=range i.request.Policy.OptionalPresets{if id==scope.Preset{selected=true}}
 config,err:=exportpreset.DecodeFrozenConfig(configRaw,scope.PresetSHA)
 if !selected||err!=nil||config.Watermark==nil{return zero,false,v2input.ErrInvalid}
 var outcome string;var raw,pin *string
 err=db.QueryRow(ctx,watermarkPinRetryQuery,scope.Tenant,scope.Project,scope.Parent,scope.Actor,currentSession,scope.Preset,scope.PresetSHA,i.key,i.requestSHA).Scan(&outcome,&raw,&pin)
 if errors.Is(err,pgx.ErrNoRows){return zero,false,v2input.ErrExpired};if err!=nil{return zero,false,err}
 if err=ctx.Err();err!=nil{return zero,false,err}
 if outcome=="absent"&&raw==nil&&pin==nil{return zero,false,nil}
 if outcome!="saved"||raw==nil||pin==nil{return zero,false,v2input.ErrInvalid}
 restored,err:=DecodeWatermarkSourcePin([]byte(*raw),*pin,scope,configRaw)
 if err!=nil{return zero,false,err};return restored,true,nil
}

package delivery

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2input"
)

// RecoverWatermarkPin restores a saved pin with current API authority and then
// performs object IO outside the database transaction. Only explicit absence
// permits initial preparation; stale or invalid evidence always returns an error.
func(s Service)RecoverWatermarkPin(ctx context.Context,who auth.Identity,tid uuid.UUID,intent CreateIntent,preset uuid.UUID,presetSHA string,configRaw []byte,bucket string,objects v2input.SessionAnalysisObjectReader)(FrozenWatermarkSource,bool,error){
 var zero FrozenWatermarkSource
 if err:=ctx.Err();err!=nil{return zero,false,err}
 if tid==uuid.Nil||intent.tenant!=tid||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||intent.actor!=who.UserID||preset==uuid.Nil||objects==nil||len(bucket)<3||len(bucket)>63{return zero,false,v2input.ErrInvalid}
 scope:=WatermarkPinScope{Tenant:tid,Project:intent.request.ProjectID,Actor:who.UserID,Parent:intent.identity.Run,Preset:preset,PresetSHA:presetSHA}
 // Own frozen config before transaction and object adapters can run.
 config:=append([]byte(nil),configRaw...)
 if s.DB==nil{return zero,false,domain.NewError("DELIVERY_UNAVAILABLE",503)}
 if err:=ValidateWatermarkAPIInstallation(ctx,s.DB);err!=nil{return zero,false,err}
 var pin FrozenWatermarkSource;var found bool
 err:=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var readErr error
  pin,found,readErr=intent.ReadWatermarkPinRetry(ctx,tx,who.SessionID,scope,config)
  return readErr
 })
 if err!=nil{return zero,false,err}
 if !found{return zero,false,nil}
 pin,err=s.ReauthorizeWatermarkPin(ctx,who,tid,scope,config,pin,bucket,objects)
 if err!=nil{return zero,false,err}
 return pin,true,nil
}

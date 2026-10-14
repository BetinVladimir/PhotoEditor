package delivery

import (
 "context"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/tenant"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

// PrepareWatermarkPin resumes saved evidence or durably selects a version before
// any object read. Each authority transaction is short; objects are read outside
// transactions. Unknown commits remain recoverable via the same create identity.
func(s Service)PrepareWatermarkPin(ctx context.Context,who auth.Identity,tid uuid.UUID,intent CreateIntent,preset uuid.UUID,presetSHA string,configRaw []byte,bucket string,objects v2input.SessionAnalysisObjectReader)(FrozenWatermarkSource,error){
 var zero FrozenWatermarkSource
 config:=append([]byte(nil),configRaw...)
 pin,found,err:=s.RecoverWatermarkPin(ctx,who,tid,intent,preset,presetSHA,config,bucket,objects)
 if err!=nil{return zero,err};if found{return pin,nil}
 scope:=WatermarkPinScope{Tenant:tid,Project:intent.request.ProjectID,Actor:who.UserID,Parent:intent.identity.Run,Preset:preset,PresetSHA:presetSHA}
 var selected WatermarkSource
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  var selectErr error;selected,selectErr=intent.SelectWatermarkVersion(ctx,tx,who.SessionID,scope,config);return selectErr
 })
 if err!=nil{return zero,err}
 // Resolve physical descriptor only after selection commit; source identity must
 // still match the frozen observation despite concurrent asset updates.
 source,err:=s.ResolveWatermarkSource(ctx,who,tid,scope.Project,selected.Asset,selected.Version,bucket)
 if err!=nil{return zero,err}
 if source.Ref!=selected.Ref{return zero,v2input.ErrExpired}
 evidence,err:=LoadWatermarkSourceEvidence(ctx,who,tid,source,objects,v2draft.SQLArtifactCatalog{DB:s.DB,Bucket:bucket,ProjectID:scope.Project,AssetVersionID:source.Version})
 if err!=nil{return zero,err}
 if err=VerifyWatermarkSourceArrays(ctx,objects,evidence);err!=nil{return zero,err}
 pin,err=CompileWatermarkSourcePin(scope,config,evidence);if err!=nil{return zero,err}
 err=tenant.WithTenant(ctx,s.DB,who,tid,domain.Editor,func(tx pgx.Tx,_ domain.Role)error{
  return intent.PinWatermarkSource(ctx,tx,who.SessionID,pin)
 })
 if err!=nil{return zero,err};if err=ctx.Err();err!=nil{return zero,err};return pin,nil
}

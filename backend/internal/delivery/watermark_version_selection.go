package delivery

import (
 "context"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/exportpreset"
 "photoeditor/backend/internal/v2input"
)
const watermarkVersionSelectionQuery=`SELECT asset_id,asset_version_id,artifact_id,sha256 FROM photo.select_v30_watermark_version($1,$2,$3,$4,$5,$6,$7,$8,$9)`
// SelectWatermarkVersion must commit before object IO. The database freezes the
// first accepted version identity and reauthorizes that same identity on retry.
func(i CreateIntent)SelectWatermarkVersion(ctx context.Context,db ProofTransaction,session uuid.UUID,scope WatermarkPinScope,configRaw []byte)(WatermarkSource,error){
 var zero WatermarkSource
 if err:=ctx.Err();err!=nil{return zero,err}
 if db==nil||session==uuid.Nil||i.tenant==uuid.Nil||i.actor==uuid.Nil||i.identity.Run==uuid.Nil||i.key==""||scope.Tenant!=i.tenant||scope.Project!=i.request.ProjectID||scope.Actor!=i.actor||scope.Parent!=i.identity.Run{return zero,v2input.ErrInvalid}
 selected:=false
 for _,id:=range i.request.Policy.RequiredPresets{if id==scope.Preset{selected=true}}
 for _,id:=range i.request.Policy.OptionalPresets{if id==scope.Preset{selected=true}}
 config,err:=exportpreset.DecodeFrozenConfig(configRaw,scope.PresetSHA)
 if !selected||err!=nil||config.Watermark==nil{return zero,v2input.ErrInvalid}
 var source WatermarkSource
 err=db.QueryRow(ctx,watermarkVersionSelectionQuery,scope.Tenant,scope.Project,scope.Parent,scope.Actor,session,scope.Preset,scope.PresetSHA,i.key,i.requestSHA).Scan(&source.Asset,&source.Version,&source.Ref.ArtifactID,&source.Ref.SHA256)
 if errors.Is(err,pgx.ErrNoRows){return zero,v2input.ErrExpired};if err!=nil{return zero,err}
 if source.Asset!=config.Watermark.AssetID||source.Version==uuid.Nil||source.Ref.ArtifactID==uuid.Nil||!shaPattern.MatchString(source.Ref.SHA256){return zero,v2input.ErrInvalid}
 if err=ctx.Err();err!=nil{return zero,err};return source,nil
}

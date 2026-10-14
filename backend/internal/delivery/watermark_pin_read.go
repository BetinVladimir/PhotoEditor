package delivery

import (
 "context"
 "errors"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)
const watermarkPinReadQuery=`SELECT pin_jcs,pin_sha FROM photo.read_v30_watermark_pin($1,$2,$3,$4,$5,$6)`
type WatermarkPinStore struct{DB ProofTransaction}
func NewWatermarkPinStore(db MetadataTransactionBeginner,tenant uuid.UUID)(WatermarkPinStore,error){
 if db==nil||tenant==uuid.Nil{return WatermarkPinStore{},v2input.ErrInvalid}
 return WatermarkPinStore{DB:MetadataTenantQueries{DB:db,Tenant:tenant}},nil
}
// Read requires independently frozen external scope/config and a current parent
// expansion fence. It returns value evidence, never an adoption permission.
func(s WatermarkPinStore)Read(ctx context.Context,scope WatermarkPinScope,expansion,token uuid.UUID,configRaw []byte)(FrozenWatermarkSource,error){
 var zero FrozenWatermarkSource
 if err:=ctx.Err();err!=nil{return zero,err}
 if s.DB==nil||scope.Tenant==uuid.Nil||scope.Project==uuid.Nil||scope.Actor==uuid.Nil||scope.Parent==uuid.Nil||scope.Preset==uuid.Nil||!shaPattern.MatchString(scope.PresetSHA)||len(configRaw)>1<<20||digest(configRaw)!=scope.PresetSHA||expansion==uuid.Nil||token==uuid.Nil{return zero,v2input.ErrInvalid}
 config:=append([]byte(nil),configRaw...)
 var raw,pin string
 err:=s.DB.QueryRow(ctx,watermarkPinReadQuery,scope.Tenant,scope.Parent,expansion,token,scope.Preset,scope.PresetSHA).Scan(&raw,&pin)
 if errors.Is(err,pgx.ErrNoRows){return zero,v2input.ErrExpired};if err!=nil{return zero,err}
 if err=ctx.Err();err!=nil{return zero,err}
 return DecodeWatermarkSourcePin([]byte(raw),pin,scope,config)
}

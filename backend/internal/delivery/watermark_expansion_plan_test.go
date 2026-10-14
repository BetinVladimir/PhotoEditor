package delivery
import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
)
type watermarkExpansionPins struct{pin FrozenWatermarkSource;scope WatermarkPinScope;calls int;revoke bool}
func(p *watermarkExpansionPins)Read(_ context.Context,scope WatermarkPinScope,expansion,token uuid.UUID,config []byte)(FrozenWatermarkSource,error){
 p.calls++
 if scope!=p.scope||expansion==uuid.Nil||token==uuid.Nil||digest(config)!=scope.PresetSHA{return FrozenWatermarkSource{},v2input.ErrInvalid}
 if p.revoke&&p.calls>1{return FrozenWatermarkSource{},v2input.ErrExpired}
 return p.pin,nil
}

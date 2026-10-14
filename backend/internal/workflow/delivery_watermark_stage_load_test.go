package workflow

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/v2input"
)
type watermarkLoaderPins struct{pin delivery.FrozenWatermarkSource;calls int;refuseSecond bool}
func(p *watermarkLoaderPins)Read(_ context.Context,_ delivery.WatermarkPinScope,_ uuid.UUID,_ uuid.UUID,_ []byte)(delivery.FrozenWatermarkSource,error){
 p.calls++;if p.refuseSecond&&p.calls>1{return delivery.FrozenWatermarkSource{},v2input.ErrExpired};return p.pin,nil
}

package delivery

import (
 "bytes"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/exportpreset"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// CompileDeliveryWatermarkForImages binds canonical source documents and the
// independently resolved watermark asset. It proves values only: caller must
// verify live tenant/project lineage, immutable objects and pixel/alpha arrays.
func CompileDeliveryWatermarkForImages(imageRef v2input.Ref,imageRaw []byte,markRef v2input.Ref,markRaw []byte,
 resolvedAsset uuid.UUID,preset exportpreset.Watermark)([]byte,string,error){
 invalid:=func()([]byte,string,error){return nil,"",v2input.ErrInvalid}
 if resolvedAsset==uuid.Nil||resolvedAsset!=preset.AssetID||digest(imageRaw)!=imageRef.SHA256||digest(markRaw)!=markRef.SHA256{return invalid()}
 image,err:=review.ParseWorkingImage(imageRaw);if err!=nil{return invalid()}
 mark,err:=review.ParseWorkingImage(markRaw);if err!=nil{return invalid()}
 imageExact,err:=canonical.Marshal(image);if err!=nil||!bytes.Equal(imageExact,imageRaw){return invalid()}
 markExact,err:=canonical.Marshal(mark);if err!=nil||!bytes.Equal(markExact,markRaw){return invalid()}
 // Existing watermark linearization is defined for working gamma24 RGB.
 if image.Color!=mark.Color||image.Color.ProfileID!="work_rec2020_gamma24_v1"||image.Color.Encoding!="gamma24"||
 image.DType!="float32"||mark.DType!="float32"||mark.Alpha==nil{return invalid()}
 return CompileDeliveryWatermarkParams(imageRef,markRef,int(image.Canvas.Width),int(image.Canvas.Height),int(mark.Canvas.Width),int(mark.Canvas.Height),preset)
}

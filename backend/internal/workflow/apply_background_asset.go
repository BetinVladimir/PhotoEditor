package workflow

import (
 "encoding/json"
 "errors"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
)

func validateApplyBackgroundAsset(pin *review.ApplyBackgroundAsset, portable string, style review.ArtifactRef) error {
 if pin==nil || pin.PortableID!=portable || pin.Style!=style || pin.StyleVersionID==uuid.Nil || !validExpansionRef(pin.Source) || pin.ExpectedBytes<1 || pin.ExpectedBytes>536870912 || (pin.Suffix!=".jpg" && pin.Suffix!=".png" && pin.Suffix!=".tiff") { return errors.New("invalid frozen portable background binding") }
 return nil
}

func applyBackgroundAssetMetadata(raw []byte, pin *review.ApplyBackgroundAsset) (IngestMetadata,error) {
 var metadata IngestMetadata
 if pin==nil || json.Unmarshal(raw,&metadata)!=nil || metadata.Source!=pin.Source || metadata.Width<1 || metadata.Height<1 || metadata.Width>200000000 || metadata.Height>200000000 || metadata.Width*metadata.Height>200000000 || metadata.HasAlpha || (metadata.Format!="jpeg" && metadata.Format!="png" && metadata.Format!="tiff") { return metadata,errors.New("unsupported background metadata or alpha") }
 return metadata,nil
}

func applyBackgroundAssetProfile(pin *review.ApplyBackgroundAsset, metadata *IngestMetadata, paramsBytes int64) (json.RawMessage,error) {
 if pin==nil || paramsBytes<1 || paramsBytes>1048576 { return nil,errors.New("invalid background decode resources") }
 pixels,output:=int64(200000000),int64(1048576)
 if metadata!=nil { pixels=metadata.Width*metadata.Height;output=pixels*12+2*1048576 }
 if pixels<1 || pixels>200000000 { return nil,errors.New("invalid background dimensions") }
 return canonical.Marshal(struct {
  Backend string `json:"backend"`; Precision string `json:"precision"`; Tile int `json:"tile_size"`; MaxPixels int64 `json:"max_pixels"`; Estimate ResourceEstimate `json:"resource_estimate"`
 }{"cpu","float32",512,200000000,ResourceEstimate{RAMBytes:1073741824+512*512*128,ScratchBytes:pin.ExpectedBytes+paramsBytes+output,OutputBytes:output,PixelCount:pixels}})
}

func validateApplyDecodedBackground(pin *review.ApplyBackgroundAsset, decoded, target review.WorkingImage) error {
 if pin==nil || decoded.DType!="float32" || decoded.Alpha!=nil || decoded.Color!=target.Color || decoded.Canvas.SourceSHA256!=pin.Source.SHA256 { return errors.New("background decode color/source binding mismatch") }
 return nil
}

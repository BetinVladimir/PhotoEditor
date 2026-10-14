package review

import (
 "bytes"
 "encoding/json"

 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/domain"
)

// This check binds review base to the original physical task Params. It does
// not replace optical qualification or validate the negative processing math.
func verifyNegativeReviewBase(paramsRaw,imageRaw []byte,base ArtifactRef)error{
 invalid:=func()error{return domain.NewError("STALE_CANDIDATE_REVIEW",409)}
 if len(paramsRaw)<2||len(paramsRaw)>65536||!base.valid(){return invalid()}
 normalized,err:=canonical.Bytes(paramsRaw);if err!=nil||!bytes.Equal(normalized,paramsRaw){return invalid()}
 var params struct{
  SchemaVersion int `json:"schema_version"`
  Base struct{Source ArtifactRef `json:"source"`;CanvasHash string `json:"canvas_hash"`} `json:"base"`
 }
 if json.Unmarshal(paramsRaw,&params)!=nil||params.SchemaVersion!=1||params.Base.Source!=base{return invalid()}
 image,err:=ParseWorkingImage(imageRaw);if err!=nil||image.DType!="float32"{return invalid()}
 canvasHash,err:=canonical.Hash(image.Canvas);if err!=nil||params.Base.CanvasHash!=canvasHash{return invalid()}
 return nil
}

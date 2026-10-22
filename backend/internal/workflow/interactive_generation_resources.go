package workflow

import (
 "context"
 "encoding/json"
 "errors"
 "photoeditor/backend/internal/review"
)

const interactiveExactIntegerLimit int64 = 1<<53 - 1

func interactiveResourceAdd(a,b int64)(int64,error){
 if a<0 || b<0 || a>interactiveExactIntegerLimit-b { return 0,errors.New("interactive resource estimate overflow") }
 return a+b,nil
}
func interactiveResourceMultiply(a,b int64)(int64,error){
 if a<0 || b<0 || (b!=0 && a>interactiveExactIntegerLimit/b) { return 0,errors.New("interactive resource estimate overflow") }
 return a*b,nil
}

// Generation publishes RGB float32, a float32 coverage plane and THREE legacy
// manifests (each stage_contract is limited to 1 MiB). Alpha is reused, not
// regenerated. Keep this paired with generate.py and generated_coverage.py.
func interactiveGenerationResources(width,height,inputBytes int64)(pixels,output,scratch,ram int64,err error){
 if width<=0 || height<=0 { err=errors.New("invalid generation canvas"); return }
 pixels,err=interactiveResourceMultiply(width,height); if err!=nil{return}
 output,err=interactiveResourceMultiply(pixels,16); if err!=nil{return}
 output,err=interactiveResourceAdd(output,3*1048576+16384+8192); if err!=nil{return}
 scratch,err=interactiveResourceAdd(inputBytes,output); if err!=nil{return}
 // Match inpaint._required_frame_ram + _roi_pixel_budget. A full-frame ROI
 // must fit up to the worker's deliberate 32M ROI limit; larger ROIs remain
 // an explicit worker refusal rather than an invented capability.
 ram,err=interactiveResourceMultiply(pixels,32); if err!=nil{return}
 ram,err=interactiveResourceAdd(ram,512*1048576); if err!=nil{return}
 roi:=pixels; if roi>32000000 {roi=32000000}
 roiBytes,e:=interactiveResourceMultiply(roi,192); if e!=nil {err=e;return}
 ram,err=interactiveResourceAdd(ram,roiBytes); if err!=nil{return}
 if ram<4<<30 {ram=4<<30}
 return
}

// Each protected entry opens its own mapped file, even when it repeats a ref.
// Count occurrences, not merely the unique command-input closure. Contract JSON
// is streamed and alpha is not mapped by generation; neither requires scratch.
func (s InteractiveStore) generationMappedBytes(ctx context.Context,l InteractiveLease,image review.WorkingImage,inputs []review.ArtifactRef)(int64,error){
 var params struct {Mask struct {Artifact review.ArtifactRef `json:"artifact"`} `json:"mask"`; Protected []struct {Artifact review.ArtifactRef `json:"artifact"`} `json:"protected"`}
 if json.Unmarshal(l.Params,&params)!=nil {return 0,errors.New("invalid generation resource params")}
 refs:=[]review.ArtifactRef{image.Pixels,params.Mask.Artifact}
 for _,m:=range params.Protected {refs=append(refs,m.Artifact)}
 var total int64
 for _,ref:=range refs {
  if err:=ctx.Err();err!=nil{return 0,err}
  declared:=false;for _,input:=range inputs {if input==ref {declared=true;break}}
  if !declared {return 0,errors.New("generation resource input undeclared")}
  sourceLease:=l;sourceLease.SourceID=ref.ID
  a,err:=s.Source(ctx,sourceLease);if err!=nil{return 0,err}
  if a.SHA256!=ref.SHA256 || a.Bytes<=0 {return 0,errors.New("generation resource input identity mismatch")}
  total,err=interactiveResourceAdd(total,a.Bytes);if err!=nil{return 0,err}
 }
 return total,ctx.Err()
}

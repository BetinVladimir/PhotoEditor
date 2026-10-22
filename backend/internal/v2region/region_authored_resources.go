package v2region

// AuthoredCropResources mirrors the existing presentation codec RAM formula,
// and adds actual declared immutable-input bytes, the real 16MiB V2 index and
// simultaneous bounded encoded/root staging. Caller derives inputBytes from the
// exact scoped catalog inventory (including Params), never request JSON. There
// is no artificial device-memory cap and no claim of measured GPU resources.
type AuthoredCropResources struct {
 PixelCount int64 `json:"pixel_count"`
 RAMBytes int64 `json:"ram_bytes"`
 ScratchBytes int64 `json:"scratch_bytes"`
 VRAMBytes int64 `json:"vram_bytes"`
 OutputBytes int64 `json:"output_bytes"`
}
func CompileAuthoredCropResources(width,height,inputBytes int64,maxEdge int,alpha bool)(AuthoredCropResources,error){
 const safe int64=9007199254740991
 if width<1||height<1||width>200000000||height>200000000/width||inputBytes<2||inputBytes>safe||maxEdge<64||maxEdge>1024{return AuthoredCropResources{},ErrRegionInvalid}
 edge:=min(int64(maxEdge),max(width,height));pixels:=width*height
 factor:=int64(24);if alpha{factor=96}
 presentation:=pixels*factor+edge*edge*96+128*1024*1024
 const output int64=25165824
 // Check before addition; even an adversarial catalog size cannot wrap int64.
 if inputBytes>safe-presentation||inputBytes>safe-(16*1024*1024+2*output+8192){return AuthoredCropResources{},ErrRegionInvalid}
 return AuthoredCropResources{PixelCount:pixels,RAMBytes:inputBytes+presentation,
  ScratchBytes:inputBytes+16*1024*1024+2*output+8192,VRAMBytes:0,OutputBytes:output},nil
}

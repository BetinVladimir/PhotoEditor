package interchange

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "io"
 "os"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

// The implementation must resolve the exact array reference from validated typed
// canvas evidence in the current export scope, including current authorization.
type ExportMaskGeometrySource interface{ResolveExportMaskGeometry(context.Context,v2input.Ref)(ExportMaskGeometry,error)}
type NativeExportMaskQualifier struct{
 Objects ImportDocumentObjects
 Scratch *ExportArchiveScratch
 Probe ExportNativeProbe
 Geometry ExportMaskGeometrySource
 RuntimeSHA string
}
func exportMaskBinaryContract(contract string)bool{return contract=="MaskArray"||contract=="MaskPixels"}

func validExportMaskGeometry(g ExportMaskGeometry,ref v2input.Ref)bool{
 return g.Width>0&&g.Height>0&&g.Width<=200000000&&g.Height<=200000000&&int64(g.Width)*int64(g.Height)<=200000000&&validImportRef(v2input.Ref{ArtifactID:ref.ArtifactID,SHA256:g.CanvasSHA})
}

func(q NativeExportMaskQualifier)QualifyExportBinary(ctx context.Context,entry v2input.Entry,object objectstore.Object)(err error){
 if err=ctx.Err();err!=nil{return}
 if q.Objects==nil||q.Scratch==nil||q.Probe==nil||q.Geometry==nil||!exportMaskBinaryContract(entry.Contract)||!validImportRef(entry.Ref)||!validImportRef(v2input.Ref{ArtifactID:entry.Ref.ArtifactID,SHA256:q.RuntimeSHA})||object.SHA256!=entry.Ref.SHA256||!exportArrayMediaType(object.ContentType)||object.Bytes<1||object.Key==""||object.VersionID==""||object.VersionID=="null"{return v2input.ErrInvalid}
 geometry,resolveErr:=q.Geometry.ResolveExportMaskGeometry(ctx,entry.Ref);if resolveErr!=nil{return resolveErr}
 if !validExportMaskGeometry(geometry,entry.Ref)||object.Bytes>int64(geometry.Width)*int64(geometry.Height)*4+exportNPYOverhead{return v2input.ErrInvalid}
 reserved:=object.Bytes+1;if !q.Scratch.reserve(reserved){return v2input.ErrInvalid}
 file,createErr:=os.CreateTemp(q.Scratch.root,"native-export-mask-*.npy");if createErr!=nil{q.Scratch.release(reserved);return createErr}
 closed:=false
 defer func(){
  var closeErr error;if !closed{closeErr=file.Close()};removeErr:=os.Remove(file.Name())
  if removeErr==nil{q.Scratch.release(reserved)}
  if err==nil{if closeErr!=nil{err=closeErr}else if removeErr!=nil{err=removeErr}}
 }()
 stream,openErr:=q.Objects.Open(ctx,object);if openErr!=nil{if stream!=nil{_ = stream.Close()};return openErr};if stream==nil{return v2input.ErrInvalid}
 digest:=sha256.New()
 count,copyErr:=io.CopyBuffer(io.MultiWriter(file,digest),io.LimitReader(exportContextReader{ctx,stream},object.Bytes+1),make([]byte,256<<10));closeErr:=stream.Close()
 if copyErr!=nil{return copyErr};if closeErr!=nil{return closeErr};if err=ctx.Err();err!=nil{return}
 if count!=object.Bytes||hex.EncodeToString(digest.Sum(nil))!=entry.Ref.SHA256{return v2input.ErrInvalid}
 if err=file.Close();err!=nil{return};closed=true
 request,marshalErr:=canonical.Marshal(struct{
  Schema int `json:"schema_version"`;Path string `json:"file_path"`;SHA string `json:"sha256"`;Bytes int64 `json:"bytes"`;Media string `json:"media_type"`;Width int `json:"width"`;Height int `json:"height"`;Canvas string `json:"canvas_hash"`
 }{1,file.Name(),entry.Ref.SHA256,object.Bytes,object.ContentType,geometry.Width,geometry.Height,geometry.CanvasSHA});if marshalErr!=nil{return marshalErr}
 raw,probeErr:=q.Probe.RunExportNativeProbe(ctx,request);if probeErr!=nil{return probeErr}
 if err=ctx.Err();err!=nil{return}
 if _,err=decodeExportNativeMaskReceipt(raw,entry,object,geometry,q.RuntimeSHA);err!=nil{return}
 current,resolveErr:=q.Geometry.ResolveExportMaskGeometry(ctx,entry.Ref);if resolveErr!=nil{return resolveErr}
 if current!=geometry{return v2input.ErrInvalid};return ctx.Err()
}
var _ ExportBinaryQualifier=NativeExportMaskQualifier{}

// Closed dispatch: unsupported contracts cannot accidentally use image or mask
// qualification. Each installed adapter still validates its exact contract.
type ExportNativeQualifierRouter struct{Images,Masks,Arrays,Backgrounds,DefectLabels,DefectPNGs,LUTs ExportBinaryQualifier}
func(r ExportNativeQualifierRouter)QualifyExportBinary(ctx context.Context,e v2input.Entry,o objectstore.Object)error{
 if err:=ctx.Err();err!=nil{return err}
 if e.Contract=="DefectLabelPNG"{if r.DefectPNGs==nil{return v2input.ErrInvalid};return r.DefectPNGs.QualifyExportBinary(ctx,e,o)}
 if e.Contract=="DefectLabelMap"&&r.DefectLabels!=nil{return r.DefectLabels.QualifyExportBinary(ctx,e,o)}
 switch e.Contract{case "EncodedImage":if r.Images!=nil{return r.Images.QualifyExportBinary(ctx,e,o)};case "MaskArray","MaskPixels":if r.Masks!=nil{return r.Masks.QualifyExportBinary(ctx,e,o)}}
 if e.Contract=="AbsoluteLUTArray"{if r.LUTs==nil{return v2input.ErrInvalid};return r.LUTs.QualifyExportBinary(ctx,e,o)}
 if exportArrayBinaryContract(e.Contract)&&r.Arrays!=nil{return r.Arrays.QualifyExportBinary(ctx,e,o)}
 if e.Contract=="StyleBackgroundAsset"&&r.Backgrounds!=nil{return r.Backgrounds.QualifyExportBinary(ctx,e,o)}
 return v2input.ErrInvalid
}

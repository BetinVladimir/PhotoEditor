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
type NativeExportImageQualifier struct{Objects ImportDocumentObjects;Scratch *ExportArchiveScratch;Probe ExportNativeProbe;RuntimeSHA string}
// Implements only EncodedImage; array/mask roles need their own qualifiers.
// Share the scratch instance with other admission IO in this process.
func(q NativeExportImageQualifier)QualifyExportBinary(ctx context.Context,entry v2input.Entry,object objectstore.Object)(err error){
 if err=ctx.Err();err!=nil{return}
 if q.Objects==nil||q.Scratch==nil||q.Probe==nil||entry.Contract!="EncodedImage"||!validImportRef(entry.Ref)||!validImportRef(v2input.Ref{ArtifactID:entry.Ref.ArtifactID,SHA256:q.RuntimeSHA})||object.SHA256!=entry.Ref.SHA256||object.Bytes<1||object.Bytes>64<<30||object.Key==""||object.VersionID==""||object.VersionID=="null"{return v2input.ErrInvalid}
 suffix,suffixErr:=exportOriginalSuffix(object.ContentType);if suffixErr!=nil{return suffixErr}
 reserved:=object.Bytes+1;if !q.Scratch.reserve(reserved){return v2input.ErrInvalid}
 file,createErr:=os.CreateTemp(q.Scratch.root,"native-export-*"+suffix);if createErr!=nil{q.Scratch.release(reserved);return createErr}
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
 request,marshalErr:=canonical.Marshal(struct{Schema int `json:"schema_version"`;Path string `json:"file_path"`;SHA string `json:"sha256"`;Bytes int64 `json:"bytes"`;Media string `json:"media_type"`}{1,file.Name(),entry.Ref.SHA256,object.Bytes,object.ContentType});if marshalErr!=nil{return marshalErr}
 raw,probeErr:=q.Probe.RunExportNativeProbe(ctx,request);if probeErr!=nil{return probeErr}
 if err=ctx.Err();err!=nil{return}
 _,err=decodeExportNativeImageReceipt(raw,entry,object,q.RuntimeSHA);return err
}
var _ ExportBinaryQualifier=NativeExportImageQualifier{}

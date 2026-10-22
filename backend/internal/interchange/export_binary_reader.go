package interchange

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "io"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

// Qualifiers must prove role-specific physical format, geometry and source
// provenance using this exact immutable object pin. Digest checks alone do not
// establish decoded image/array validity or permission to use processing models.
type ExportBinaryQualifier interface{QualifyExportBinary(context.Context,v2input.Entry,objectstore.Object)error}
type ExportBinaryNodeResolver struct{Catalog ExportDocumentCatalog;Objects ImportDocumentObjects;Qualifier ExportBinaryQualifier;MaxBytes int64}
func exportBinaryContract(contract string)bool{
 switch contract{case "EncodedImage","StyleBackgroundAsset","WorkingPixels","WorkingAlpha","PixelArray","MaskPixels","MaskArray","DenseWarpField","DenseWarpArray","DefectLabelMap","DefectLabelPNG","AbsoluteLUTArray":return true};return false
}
func(r ExportBinaryNodeResolver)ResolveExportNode(ctx context.Context,entry v2input.Entry)(ExportClosureNode,error){
 var zero ExportClosureNode
 if err:=ctx.Err();err!=nil{return zero,err}
 if !exportBinaryContract(entry.Contract)||!validImportRef(entry.Ref)||r.Catalog==nil||r.Objects==nil||r.Qualifier==nil||r.MaxBytes<1||r.MaxBytes>128<<30{return zero,v2input.ErrInvalid}
 object,err:=r.Catalog.LookupExportDocument(ctx,entry);if err!=nil{return zero,err}
 mediaOK:=object.ContentType=="application/octet-stream"
 if exportArrayBinaryContract(entry.Contract)||exportMaskBinaryContract(entry.Contract)||entry.Contract=="DefectLabelMap"{mediaOK=exportArrayMediaType(object.ContentType)}
 if entry.Contract=="DefectLabelPNG"{mediaOK=object.ContentType=="image/png"}
 if entry.Contract=="EncodedImage"||entry.Contract=="StyleBackgroundAsset"{mediaOK=object.ContentType=="image/tiff"||object.ContentType=="image/jpeg"||object.ContentType=="image/png"}
 if !mediaOK||object.Bytes<1||object.Bytes>r.MaxBytes||object.SHA256!=entry.Ref.SHA256||object.Key==""||object.VersionID==""||object.VersionID=="null"{return zero,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err}
 stream,err:=r.Objects.Open(ctx,object)
 if err!=nil{if stream!=nil{_ = stream.Close()};return zero,err};if stream==nil{return zero,v2input.ErrInvalid}
 digest:=sha256.New();limited:=io.LimitReader(stream,object.Bytes+1);buffer:=make([]byte,256<<10);var count int64
 for{
  if err:=ctx.Err();err!=nil{_ = stream.Close();return zero,err}
  n,readErr:=limited.Read(buffer)
  if n>0{_,_ = digest.Write(buffer[:n]);count+=int64(n)}
  if readErr==io.EOF{break};if readErr!=nil{_ = stream.Close();return zero,readErr}
  if n==0{_ = stream.Close();return zero,io.ErrNoProgress}
 }
 if err:=stream.Close();err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 if count!=object.Bytes||hex.EncodeToString(digest.Sum(nil))!=entry.Ref.SHA256{return zero,v2input.ErrInvalid}
 if err:=r.Qualifier.QualifyExportBinary(ctx,entry,object);err!=nil{return zero,err}
 if err:=ctx.Err();err!=nil{return zero,err}
 current,err:=r.Catalog.LookupExportDocument(ctx,entry);if err!=nil{return zero,err}
 if current!=object{return zero,v2input.ErrInvalid}
 return ExportClosureNode{Entry:entry},ctx.Err()
}

type ExportNodeResolverRouter struct{JSON,Binary ExportClosureNodeResolver}
func(r ExportNodeResolverRouter)ResolveExportNode(ctx context.Context,entry v2input.Entry)(ExportClosureNode,error){
 selected:=r.JSON;if exportBinaryContract(entry.Contract){selected=r.Binary}
 if selected==nil{return ExportClosureNode{},v2input.ErrInvalid}
 return selected.ResolveExportNode(ctx,entry)
}

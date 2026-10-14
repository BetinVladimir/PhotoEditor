package interchange

import (
 "bytes"
 "context"
 "io"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
type ImportBackgroundRasterQualifier interface{QualifyImportBackgroundRaster(context.Context,v2input.Ref,objectstore.Object)(string,error)}
type NativeImportBackgroundQualifier struct{Native NativeExportImageQualifier}
// Rewrite only the decoder's media hint. Object reads retain the exact staged
// metadata and immutable version; no object metadata is forged at storage IO.
type importBackgroundNativeObjects struct{objects ImportDocumentObjects;actual,decoder objectstore.Object}
func(o importBackgroundNativeObjects)Open(ctx context.Context,object objectstore.Object)(io.ReadCloser,error){
 if object!=o.decoder{return nil,v2input.ErrInvalid};return o.objects.Open(ctx,o.actual)
}
func(q NativeImportBackgroundQualifier)QualifyImportBackgroundRaster(ctx context.Context,ref v2input.Ref,object objectstore.Object)(string,error){
 if err:=ctx.Err();err!=nil{return "",err}
 if q.Native.Objects==nil||q.Native.Scratch==nil||q.Native.Probe==nil||!validImportRef(v2input.Ref{ArtifactID:ref.ArtifactID,SHA256:q.Native.RuntimeSHA})||!validImportRef(ref)||object.SHA256!=ref.SHA256||object.Key==""||object.VersionID==""||object.VersionID=="null"||object.ContentType!="application/octet-stream"||object.Bytes<8||object.Bytes>536870912{return "",v2input.ErrInvalid}
 stream,err:=q.Native.Objects.Open(ctx,object);if err!=nil{if stream!=nil{_ = stream.Close()};return "",err};if stream==nil{return "",v2input.ErrInvalid}
 header:=make([]byte,8);_,readErr:=io.ReadFull(exportContextReader{ctx,stream},header);closeErr:=stream.Close()
 if readErr!=nil{return "",readErr};if closeErr!=nil{return "",closeErr}
 media:=""
 switch{
 case bytes.Equal(header,[]byte{137,80,78,71,13,10,26,10}):media="image/png"
 case header[0]==255&&header[1]==216&&header[2]==255:media="image/jpeg"
 case bytes.Equal(header[:4],[]byte{'I','I',42,0})||bytes.Equal(header[:4],[]byte{'M','M',0,42})||bytes.Equal(header[:4],[]byte{'I','I',43,0})||bytes.Equal(header[:4],[]byte{'M','M',0,43}):media="image/tiff"
 default:return "",v2input.ErrInvalid
 }
 decoder:=object;decoder.ContentType=media
 native:=q.Native;native.Objects=importBackgroundNativeObjects{objects:q.Native.Objects,actual:object,decoder:decoder}
 if err:=native.QualifyExportBinary(ctx,v2input.Entry{Ref:ref,Contract:"EncodedImage"},decoder);err!=nil{return "",err}
 return media,ctx.Err()
}

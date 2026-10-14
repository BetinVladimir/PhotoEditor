package interchange

import (
 "context"
 "io"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type ImportReviewParsedSource interface{ResolveReviewParsed(context.Context)(v2input.Ref,objectstore.Object,error)}
func ReadImportReviewParsed(ctx context.Context,source ImportReviewParsedSource,objects ImportDocumentObjects)([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if source==nil||objects==nil{return nil,v2input.ErrInvalid}
 ref,object,err:=source.ResolveReviewParsed(ctx);if err!=nil{return nil,err}
 if !validImportRef(ref)||object.SHA256!=ref.SHA256||object.Key==""||object.VersionID==""||object.VersionID=="null"||object.ContentType!="application/json"||object.Bytes<2||object.Bytes>8<<20{return nil,v2input.ErrInvalid}
 reader,err:=objects.Open(ctx,object);if err!=nil{if reader!=nil{_ = reader.Close()};return nil,err};if reader==nil{return nil,v2input.ErrInvalid}
 raw,readErr:=io.ReadAll(io.LimitReader(exportContextReader{ctx,reader},object.Bytes+1));closeErr:=reader.Close()
 if readErr!=nil{return nil,readErr};if closeErr!=nil{return nil,closeErr};if err:=ctx.Err();err!=nil{return nil,err}
 if int64(len(raw))!=object.Bytes||rawSHA(raw)!=ref.SHA256{return nil,v2input.ErrInvalid}
 if _,err:=validateParsedImportDocument(ctx,raw);err!=nil{return nil,err}
 currentRef,current,err:=source.ResolveReviewParsed(ctx);if err!=nil{return nil,err}
 if currentRef!=ref||current!=object{return nil,v2input.ErrInvalid}
 return raw,ctx.Err()
}

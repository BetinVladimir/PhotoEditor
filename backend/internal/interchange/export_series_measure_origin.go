package interchange

import (
 "context"
 "io"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type ExportSeriesMeasureOrigin struct{Task uuid.UUID;Attempt int;Fence int64;Params v2input.Ref;Object objectstore.Object}
type ExportSeriesMeasureOriginSource interface{LookupExportSeriesMeasureOrigin(context.Context,ExportSeriesOriginEvidence,v2input.Entry,[]byte)(ExportSeriesMeasureOrigin,error)}
type ExportSeriesMeasureParamsReader struct{Source ExportSeriesMeasureOriginSource;Objects ImportDocumentObjects}
func(r ExportSeriesMeasureParamsReader)ReadExportSeriesMeasureParams(ctx context.Context,balance ExportSeriesOriginEvidence,entry v2input.Entry,raw []byte)(ExportSeriesMeasureOrigin,[]byte,error){
 var zero ExportSeriesMeasureOrigin
 if err:=ctx.Err();err!=nil{return zero,nil,err}
 if r.Source==nil||r.Objects==nil||entry.Contract!="SeriesStats"||!validImportRef(entry.Ref)||len(raw)<2||len(raw)>1<<20||rawSHA(raw)!=entry.Ref.SHA256{return zero,nil,v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 origin,err:=r.Source.LookupExportSeriesMeasureOrigin(ctx,balance,entry,append([]byte(nil),body...));if err!=nil{return zero,nil,err}
 object:=origin.Object
 if origin.Task==uuid.Nil||origin.Attempt<1||origin.Fence<1||!validImportRef(origin.Params)||object.SHA256!=origin.Params.SHA256||object.ContentType!="application/json"||object.Bytes<2||object.Bytes>1<<20||object.Key==""||object.VersionID==""||object.VersionID=="null"{return zero,nil,v2input.ErrInvalid}
 stream,err:=r.Objects.Open(ctx,object);if err!=nil{if stream!=nil{_ = stream.Close()};return zero,nil,err};if stream==nil{return zero,nil,v2input.ErrInvalid}
 params,readErr:=io.ReadAll(io.LimitReader(exportContextReader{ctx,stream},object.Bytes+1));closeErr:=stream.Close()
 if readErr!=nil{return zero,nil,readErr};if closeErr!=nil{return zero,nil,closeErr}
 if int64(len(params))!=object.Bytes||rawSHA(params)!=origin.Params.SHA256{return zero,nil,v2input.ErrInvalid}
 current,err:=r.Source.LookupExportSeriesMeasureOrigin(ctx,balance,entry,append([]byte(nil),body...));if err!=nil{return zero,nil,err};if current!=origin{return zero,nil,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,nil,err};return origin,params,nil
}

package interchange
import (
 "context"
 "io"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

// Evidence must be derived from the current completed stats producer, never
// selected by an archive. Params object must be authorized in that origin.
type ExportStatsOriginEvidence struct {Params v2input.Ref;Object objectstore.Object}
type ExportStatsOriginSource interface {
 LookupExportStatsOrigin(context.Context,v2input.Entry,[]byte)(ExportStatsOriginEvidence,error)
}
type ExportStatsPersonAuthority interface {
 QualifyExportStatsPerson(context.Context,v2input.Entry,v2input.Ref,[]byte,review.StatsParams,review.WorkingImage)error
}
type ExportStatsOriginQualifier struct {
 Origin ExportStatsOriginSource
 Objects ImportDocumentObjects
 Images ExportClosureNodeResolver
 Person ExportStatsPersonAuthority
}
func(q ExportStatsOriginQualifier)QualifyExportStats(ctx context.Context,entry v2input.Entry,raw []byte)error {
 if err:=ctx.Err();err!=nil{return err}
 if q.Origin==nil||q.Objects==nil||q.Images==nil||q.Person==nil||entry.Contract!="Stats20"||!validImportRef(entry.Ref)||rawSHA(raw)!=entry.Ref.SHA256{return v2input.ErrInvalid}
 stats,err:=review.ParseStats20(raw);if err!=nil||stats.SourceImage==nil{return v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 evidence,err:=q.Origin.LookupExportStatsOrigin(ctx,entry,append([]byte(nil),body...));if err!=nil{return err}
 object:=evidence.Object
 if !validImportRef(evidence.Params)||object.SHA256!=evidence.Params.SHA256||object.ContentType!="application/json"||object.Bytes<2||object.Bytes>1<<20||object.Key==""||object.VersionID==""||object.VersionID=="null"{return v2input.ErrInvalid}
 stream,err:=q.Objects.Open(ctx,object);if err!=nil{if stream!=nil{_ = stream.Close()};return err};if stream==nil{return v2input.ErrInvalid}
 paramsRaw,readErr:=io.ReadAll(io.LimitReader(exportContextReader{ctx,stream},object.Bytes+1));closeErr:=stream.Close()
 if readErr!=nil{return readErr};if closeErr!=nil{return closeErr}
 if int64(len(paramsRaw))!=object.Bytes||rawSHA(paramsRaw)!=evidence.Params.SHA256{return v2input.ErrInvalid}
 params,err:=review.ParseStatsParams(paramsRaw);if err!=nil{return err}
 imageEntry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:stats.SourceImage.ID,SHA256:stats.SourceImage.SHA256},Contract:"WorkingImage"}
 node,err:=q.Images.ResolveExportNode(ctx,imageEntry);if err!=nil{return err}
 if node.Entry!=imageEntry||rawSHA(node.Body)!=imageEntry.Ref.SHA256{return v2input.ErrInvalid}
 image,err:=review.ParseWorkingImage(node.Body);if err!=nil{return err}
 if err:=params.ValidateSource(image,*stats.SourceImage);err!=nil{return err}
 if stats.MaskConfidence!=params.Person.Confidence{return v2input.ErrInvalid}
 if err:=q.Person.QualifyExportStatsPerson(ctx,entry,evidence.Params,append([]byte(nil),paramsRaw...),params,image);err!=nil{return err}
 current,err:=q.Origin.LookupExportStatsOrigin(ctx,entry,append([]byte(nil),body...));if err!=nil{return err}
 if current!=evidence{return v2input.ErrInvalid}
 return ctx.Err()
}
var _ ExportStatsAuthority=ExportStatsOriginQualifier{}

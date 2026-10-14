package interchange

import (
 "context"
 "io"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

// Implementations authorize each original producer document in the current
// tenant/project and origin, not merely by its hash. Contracts are explicit;
// original PersonParams are not arbitrary archive Params.
type ExportStatsPersonDocumentCatalog interface {
 LookupExportStatsPersonDocument(context.Context,ExportStatsPersonOrigin,v2input.Entry)(objectstore.Object,error)
}
type ExportStatsPersonDocumentReader struct {
 Catalog ExportStatsPersonDocumentCatalog
 Objects ImportDocumentObjects
}
func(r ExportStatsPersonDocumentReader)ReadExportStatsPersonDocuments(ctx context.Context,origin ExportStatsPersonOrigin)(ExportStatsPersonDocuments,error){
 var zero,out ExportStatsPersonDocuments
 if err:=ctx.Err();err!=nil{return zero,err}
 if r.Catalog==nil||r.Objects==nil||!validImportRef(origin.Root)||!validImportRef(origin.Params)||!validImportRef(origin.Report)||origin.Root.ArtifactID==origin.Params.ArtifactID||origin.Root.ArtifactID==origin.Report.ArtifactID||origin.Params.ArtifactID==origin.Report.ArtifactID{return zero,v2input.ErrInvalid}
 items:=[]struct{entry v2input.Entry;maximum int64;target *[]byte}{
  {v2input.Entry{Ref:origin.Root,Contract:"MaskRef"},1<<20,&out.Root},
  {v2input.Entry{Ref:origin.Params,Contract:"PersonParams"},65536,&out.Params},
  {v2input.Entry{Ref:origin.Report,Contract:"PersonReport"},1<<20,&out.Report},
 }
 pins:=make([]objectstore.Object,len(items))
 // Authorize and bound every document before opening any stream.
 for i,item:=range items{
  object,err:=r.Catalog.LookupExportStatsPersonDocument(ctx,origin,item.entry);if err!=nil{return zero,err}
  if object.SHA256!=item.entry.Ref.SHA256||object.ContentType!="application/json"||object.Bytes<2||object.Bytes>item.maximum||object.Key==""||object.VersionID==""||object.VersionID=="null"{return zero,v2input.ErrInvalid}
  pins[i]=object
 }
 for i,item:=range items{
  object:=pins[i]
  stream,err:=r.Objects.Open(ctx,object);if err!=nil{if stream!=nil{_ = stream.Close()};return zero,err};if stream==nil{return zero,v2input.ErrInvalid}
  body,readErr:=io.ReadAll(io.LimitReader(exportContextReader{ctx,stream},object.Bytes+1));closeErr:=stream.Close()
  if readErr!=nil{return zero,readErr};if closeErr!=nil{return zero,closeErr}
  if int64(len(body))!=object.Bytes||rawSHA(body)!=item.entry.Ref.SHA256{return zero,v2input.ErrInvalid}
  *item.target=body
 }
 // Recheck the whole set after the last stream, including the first document.
 // No partially read evidence escapes if any authority or pin has changed.
 for i,item:=range items{
  current,err:=r.Catalog.LookupExportStatsPersonDocument(ctx,origin,item.entry);if err!=nil{return zero,err}
  if current!=pins[i]{return zero,v2input.ErrInvalid}
 }
 if err:=ctx.Err();err!=nil{return zero,err};return out,nil
}

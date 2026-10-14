package interchange

import (
 "context"
 "io"
 "github.com/google/uuid"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

// ExportOutputCatalog must authorize ready outputs of the exact frozen export
// task, attempt and tenant with current ACL. The draft input catalog is not an
// implementation of this interface. Every lookup must recheck current authority.
type ExportOutputCatalog interface {
 LookupExportOutput(context.Context,v2input.Entry)(objectstore.Object,error)
}
type ExportResultReader struct {Catalog ExportOutputCatalog;Objects ImportDocumentObjects}
type exportContextReader struct {ctx context.Context;reader io.Reader}
func(r exportContextReader)Read(p []byte)(int,error){if err:=r.ctx.Err();err!=nil{return 0,err};return r.reader.Read(p)}

// Read validates immutable JSON receipts outside catalog transactions and then
// rechecks all pins. The returned receipt is not authority to complete a run:
// ZIP qualification and atomic adoption must still recheck the same outputs.
func(r ExportResultReader)Read(ctx context.Context,intent CreateIntent,frozen FrozenExportDocuments,root v2input.Ref)(ValidatedExportResult,error){
 var zero ValidatedExportResult
 if err:=ctx.Err();err!=nil{return zero,err}
 if r.Catalog==nil||r.Objects==nil||!validImportRef(root){return zero,v2input.ErrInvalid}
 if _,_,err:=DecodeFrozenExportDocuments(ctx,intent,frozen.Body());err!=nil{return zero,err}
 type pin struct{entry v2input.Entry;object objectstore.Object}
 pins:=[]pin{};seen:=map[uuid.UUID]bool{};mappingBytes:=int64(0)
 read:=func(ref v2input.Ref,contract string,max int64)(ExportResultDocumentBytes,error){
  if err:=ctx.Err();err!=nil{return ExportResultDocumentBytes{},err}
  if !validImportRef(ref)||seen[ref.ArtifactID]{return ExportResultDocumentBytes{},v2input.ErrInvalid};seen[ref.ArtifactID]=true
  entry:=v2input.Entry{Ref:ref,Contract:contract}
  object,err:=r.Catalog.LookupExportOutput(ctx,entry);if err!=nil{return ExportResultDocumentBytes{},err}
  if object.Key==""||object.VersionID==""||object.VersionID=="null"||object.ContentType!="application/json"||object.SHA256!=ref.SHA256||object.Bytes<2||object.Bytes>max{return ExportResultDocumentBytes{},v2input.ErrInvalid}
  if contract!="InterchangeResult"{mappingBytes+=object.Bytes;if mappingBytes>8<<20{return ExportResultDocumentBytes{},v2input.ErrInvalid}}
  stream,err:=r.Objects.Open(ctx,object);if err!=nil{return ExportResultDocumentBytes{},err};if stream==nil{return ExportResultDocumentBytes{},v2input.ErrInvalid}
  raw,readErr:=io.ReadAll(io.LimitReader(exportContextReader{ctx,stream},object.Bytes+1));closeErr:=stream.Close()
  if err:=ctx.Err();err!=nil{return ExportResultDocumentBytes{},err};if readErr!=nil{return ExportResultDocumentBytes{},readErr};if closeErr!=nil{return ExportResultDocumentBytes{},closeErr}
  if int64(len(raw))!=object.Bytes||rawSHA(raw)!=ref.SHA256{return ExportResultDocumentBytes{},v2input.ErrInvalid}
  pins=append(pins,pin{entry,object})
  return ExportResultDocumentBytes{Ref:ref,Body:raw},nil
 }
 rootDoc,err:=read(root,"InterchangeResult",64<<10);if err!=nil{return zero,err}
 var result exportResultDocument;if err:=decodeImportDocument(rootDoc.Body,&result);err!=nil{return zero,err}
 if result.Schema!=1||result.Bundle==nil||!validImportRef(*result.Bundle)||result.Drafts==nil||result.Imported==nil||len(result.Drafts)!=0||len(result.Imported)!=0{return zero,v2input.ErrInvalid}
 reportDoc,err:=read(result.Report,"MappingReport",8<<20);if err!=nil{return zero,err}
 var report exportMappingReport;if err:=decodeImportDocument(reportDoc.Body,&report);err!=nil{return zero,err}
 if report.Schema!=1||report.Count<1||report.Count>5000||len(report.Shards)!=(report.Count+99)/100||len(report.Shards)>50{return zero,v2input.ErrInvalid}
 shards:=make([]ExportResultDocumentBytes,0,len(report.Shards))
 for _,ref:=range report.Shards{doc,err:=read(ref,"MappingShard",8<<20);if err!=nil{return zero,err};shards=append(shards,doc)}
 receipt,err:=ValidateExportResultDocuments(ctx,intent,frozen,rootDoc,reportDoc,shards);if err!=nil{return zero,err}
 for _,p:=range pins{
  if err:=ctx.Err();err!=nil{return zero,err}
  current,err:=r.Catalog.LookupExportOutput(ctx,p.entry);if err!=nil{return zero,err};if current!=p.object{return zero,v2input.ErrInvalid}
 }
 return receipt,ctx.Err()
}

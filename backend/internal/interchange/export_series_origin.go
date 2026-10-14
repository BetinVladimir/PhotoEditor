package interchange

import (
 "context"
 "encoding/json"
 "io"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)

type ExportSeriesOriginEvidence struct{Task uuid.UUID;Attempt int;Fence int64;Params v2input.Ref;Object objectstore.Object}
type ExportSeriesOriginSource interface{
 LookupExportSeriesOrigin(context.Context,v2input.Entry,[]byte)(ExportSeriesOriginEvidence,error)
 LookupExportSeriesOriginDocument(context.Context,ExportSeriesOriginEvidence,v2input.Entry)(objectstore.Object,error)
}
type exportSeriesStatPin struct{ID uuid.UUID `json:"artifact_id"`;SHA string `json:"sha256"`;Body json.RawMessage `json:"body"`}
type exportSeriesOriginBundle struct{Schema int `json:"schema_version"`;Corrections exportCompositionDocument `json:"corrections"`;Params exportCompositionDocument `json:"params"`;Stats []exportSeriesStatPin `json:"stats"`}

type ExportSeriesOriginReader struct{Source ExportSeriesOriginSource;Objects ImportDocumentObjects}
func(r ExportSeriesOriginReader)ReadExportSeriesOrigin(ctx context.Context,entry v2input.Entry,raw []byte)([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if r.Source==nil||r.Objects==nil||entry.Contract!="SeriesCorrections"||!validImportRef(entry.Ref)||len(raw)<2||len(raw)>8<<20||rawSHA(raw)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 origin,err:=r.Source.LookupExportSeriesOrigin(ctx,entry,append([]byte(nil),body...));if err!=nil{return nil,err}
 if origin.Task==uuid.Nil||origin.Attempt<1||origin.Fence<1||!validImportRef(origin.Params){return nil,v2input.ErrInvalid}
 valid:=func(object objectstore.Object,ref v2input.Ref)bool{return validImportRef(ref)&&object.SHA256==ref.SHA256&&object.ContentType=="application/json"&&object.Bytes>=2&&object.Bytes<=1<<20&&object.Key!=""&&object.VersionID!=""&&object.VersionID!="null"}
 remaining:=int64(24<<20)
 read:=func(object objectstore.Object,ref v2input.Ref)([]byte,error){
  if !valid(object,ref)||object.Bytes>remaining{return nil,v2input.ErrInvalid};remaining-=object.Bytes
  stream,err:=r.Objects.Open(ctx,object);if err!=nil{if stream!=nil{_ = stream.Close()};return nil,err};if stream==nil{return nil,v2input.ErrInvalid}
  data,readErr:=io.ReadAll(io.LimitReader(exportContextReader{ctx,stream},object.Bytes+1));closeErr:=stream.Close()
  if readErr!=nil{return nil,readErr};if closeErr!=nil{return nil,closeErr}
  if int64(len(data))!=object.Bytes||rawSHA(data)!=ref.SHA256{return nil,v2input.ErrInvalid};return data,ctx.Err()
 }
 params,err:=read(origin.Object,origin.Params);if err!=nil{return nil,err}
 var projection struct{Stats []v2input.Ref `json:"stats"`}
 if json.Unmarshal(params,&projection)!=nil||len(projection.Stats)<2||len(projection.Stats)>2000{return nil,v2input.ErrInvalid}
 // Authorize the entire stats inventory and aggregate budget before stats IO.
 objects:=make([]objectstore.Object,len(projection.Stats));seen:=map[uuid.UUID]bool{origin.Params.ArtifactID:true,entry.Ref.ArtifactID:true};total:=int64(0)
 for i,ref:=range projection.Stats{
  if !validImportRef(ref)||seen[ref.ArtifactID]{return nil,v2input.ErrInvalid};seen[ref.ArtifactID]=true
  object,err:=r.Source.LookupExportSeriesOriginDocument(ctx,origin,v2input.Entry{Ref:ref,Contract:"SeriesStats"});if err!=nil{return nil,err}
  if !valid(object,ref){return nil,v2input.ErrInvalid};total+=object.Bytes;if total>remaining{return nil,v2input.ErrInvalid};objects[i]=object
 }
 bundle:=exportSeriesOriginBundle{Schema:1,Corrections:exportCompositionDocument{SHA:entry.Ref.SHA256,Body:body},Params:exportCompositionDocument{SHA:origin.Params.SHA256,Body:params},Stats:make([]exportSeriesStatPin,0,len(objects))}
 for i,ref:=range projection.Stats{
  data,err:=read(objects[i],ref);if err!=nil{return nil,err}
  bundle.Stats=append(bundle.Stats,exportSeriesStatPin{ID:ref.ArtifactID,SHA:ref.SHA256,Body:data})
 }
 current,err:=r.Source.LookupExportSeriesOrigin(ctx,entry,append([]byte(nil),body...));if err!=nil{return nil,err};if current!=origin{return nil,v2input.ErrInvalid}
 for i,ref:=range projection.Stats{current,err:=r.Source.LookupExportSeriesOriginDocument(ctx,origin,v2input.Entry{Ref:ref,Contract:"SeriesStats"});if err!=nil{return nil,err};if current!=objects[i]{return nil,v2input.ErrInvalid}}
 encoded,err:=canonical.Marshal(bundle);if err!=nil{return nil,err};if len(encoded)>32<<20{return nil,v2input.ErrInvalid};return encoded,ctx.Err()
}

package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "io"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/v2input"
)

type ExportSeriesMaskOriginReader struct{Source ExportSeriesMaskOriginSource;Objects ImportDocumentObjects}
func(r ExportSeriesMaskOriginReader)ReadExportSeriesMaskOrigin(ctx context.Context,measure ExportSeriesMeasureOrigin,stat v2input.Entry,raw []byte,image ExportClosureNode,ref v2input.Ref)(ExportSeriesMaskOrigin,[]byte,error){
 var zero ExportSeriesMaskOrigin
 if err:=ctx.Err();err!=nil{return zero,nil,err}
 if r.Source==nil||r.Objects==nil||!validImportRef(ref){return zero,nil,v2input.ErrInvalid}
 if _,err:=exportSeriesMeasureInputRefs(measure,raw,image);err!=nil{return zero,nil,err}
 body:=append([]byte(nil),raw...);imageBody:=append([]byte(nil),image.Body...)
 readOrigin:=func()(ExportSeriesMaskOrigin,error){return r.Source.LookupExportSeriesMaskOrigin(ctx,measure,stat,append([]byte(nil),body...),ExportClosureNode{Entry:image.Entry,Body:append([]byte(nil),imageBody...),Children:append([]v2input.Entry(nil),image.Children...)},ref)}
 origin,err:=readOrigin();if err!=nil{return zero,nil,err}
 if origin.Task==uuid.Nil||origin.Attempt<1||origin.Fence<1||origin.Version!="1"||(origin.Operation!="mask.person"&&origin.Operation!="mask.prompt")||origin.Mask.SHA256!=ref.SHA256{return zero,nil,v2input.ErrInvalid}
 documents:=[]ExportSeriesMaskDocument{origin.Root,origin.Params,origin.Report};seen:=map[uuid.UUID]bool{ref.ArtifactID:true}
 for _,doc:=range documents{object:=doc.Object;if !validImportRef(doc.Ref)||seen[doc.Ref.ArtifactID]||object.SHA256!=doc.Ref.SHA256||object.ContentType!="application/json"||object.Bytes<2||object.Bytes>1<<20||object.Key==""||object.VersionID==""||object.VersionID=="null"{return zero,nil,v2input.ErrInvalid};seen[doc.Ref.ArtifactID]=true}
 pins:=make([]exportCompositionDocument,0,3)
 for _,doc:=range documents{
  stream,err:=r.Objects.Open(ctx,doc.Object);if err!=nil{if stream!=nil{_ = stream.Close()};return zero,nil,err};if stream==nil{return zero,nil,v2input.ErrInvalid}
  data,readErr:=io.ReadAll(io.LimitReader(exportContextReader{ctx,stream},doc.Object.Bytes+1));closeErr:=stream.Close()
  if readErr!=nil{return zero,nil,readErr};if closeErr!=nil{return zero,nil,closeErr};if int64(len(data))!=doc.Object.Bytes||rawSHA(data)!=doc.Ref.SHA256{return zero,nil,v2input.ErrInvalid}
  pins=append(pins,exportCompositionDocument{SHA:doc.Ref.SHA256,Body:data})
 }
 var params struct{Regions []struct{Mask json.RawMessage `json:"mask"`} `json:"regions"`}
 if json.Unmarshal(body,&params)!=nil{return zero,nil,v2input.ErrInvalid};found:=false
 for _,region:=range params.Regions{mask,err:=review.ParseMaskRef(region.Mask);if err!=nil{return zero,nil,err};if mask.Artifact.ID==ref.ArtifactID&&mask.Artifact.SHA256==ref.SHA256{inline,err:=canonical.Bytes(region.Mask);if err!=nil||!bytes.Equal(inline,pins[0].Body){return zero,nil,v2input.ErrInvalid};found=true}}
 if !found{return zero,nil,v2input.ErrInvalid}
 current,err:=readOrigin();if err!=nil{return zero,nil,err};if current!=origin{return zero,nil,v2input.ErrInvalid}
 bundle,err:=canonical.Marshal(struct{Schema int `json:"schema_version"`;Operation string `json:"operation"`;Version string `json:"version"`;Root exportCompositionDocument `json:"root"`;Params exportCompositionDocument `json:"params"`;Report exportCompositionDocument `json:"report"`;Image exportCompositionDocument `json:"image"`}{1,origin.Operation,origin.Version,pins[0],pins[1],pins[2],exportCompositionDocument{SHA:image.Entry.Ref.SHA256,Body:imageBody}})
 if err!=nil{return zero,nil,err};if len(bundle)>12<<20{return zero,nil,v2input.ErrInvalid};return origin,bundle,ctx.Err()
}

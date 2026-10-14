package interchange

import (
 "bytes"
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

type exportBodyOriginBundle struct{
 Schema int `json:"schema_version"`
 Params exportCompositionDocument `json:"params"`
 Image exportCompositionDocument `json:"image"`
 Poses exportCompositionDocument `json:"poses"`
 Fit *exportCompositionDocument `json:"fit"`
 Faces *exportCompositionDocument `json:"faces"`
}
type ExportBodyOriginReader struct{Source ExportParamsOperationSource;Documents ExportClosureNodeResolver}

// Produces content evidence only. Caller must run BodyOrigin native schema and
// current model/consent authority, then repeat this reader after native IO.
func(r ExportBodyOriginReader)ReadExportBodyOrigin(ctx context.Context,entry v2input.Entry,raw []byte)(ExportParamsOperationEvidence,[]byte,error){
 var zero ExportParamsOperationEvidence
 if err:=ctx.Err();err!=nil{return zero,nil,err}
 if r.Source==nil||r.Documents==nil||entry.Contract!="Params"||!validImportRef(entry.Ref)||len(raw)<2||len(raw)>1<<20||rawSHA(raw)!=entry.Ref.SHA256{return zero,nil,v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 operation,err:=exportParamsOperationShape(body);if err!=nil||operation!="geometry.body"{return zero,nil,v2input.ErrInvalid}
 refs,err:=v2draft.DocumentChildren(body,"Params",entry.Ref);if err!=nil{return zero,nil,err}
 origin,err:=r.Source.LookupExportParamsOperation(ctx,entry,append([]byte(nil),body...));if err!=nil{return zero,nil,err}
 if origin.Task==uuid.Nil||origin.Operation!="geometry.body"||origin.Version!="1"||origin.Attempt<1||origin.Fence<1{return zero,nil,v2input.ErrInvalid}
 bundle:=exportBodyOriginBundle{Schema:1,Params:exportCompositionDocument{SHA:entry.Ref.SHA256,Body:append(json.RawMessage(nil),body...)}}
 entries:=[]v2input.Entry{};documents:=[]exportCompositionDocument{};seen:=map[uuid.UUID]v2input.Entry{entry.Ref.ArtifactID:entry}
 remaining:=int64(10<<20)-int64(len(body))
 for _,ref:=range refs{
  var target *exportCompositionDocument
  switch ref.Path{case "Document.image":target=&bundle.Image;case "Document.poses":target=&bundle.Poses;case "Document.fit":bundle.Fit=&exportCompositionDocument{};target=bundle.Fit;case "Document.faces":bundle.Faces=&exportCompositionDocument{};target=bundle.Faces;default:continue}
  expected:=v2input.Entry{Ref:ref.Ref,Contract:ref.Contract}
  if _,exists:=seen[expected.Ref.ArtifactID];exists{return zero,nil,v2input.ErrInvalid};seen[expected.Ref.ArtifactID]=expected
  node,err:=r.Documents.ResolveExportNode(ctx,expected);if err!=nil{return zero,nil,err}
  document,err:=ownExportCompositionDocument(ctx,node,expected);if err!=nil{return zero,nil,err}
  if len(document.Body)>2<<20{return zero,nil,v2input.ErrInvalid};remaining-=int64(len(document.Body));if remaining<0{return zero,nil,v2input.ErrInvalid}
  *target=document;entries=append(entries,expected);documents=append(documents,document)
 }
 if len(bundle.Image.Body)==0||len(bundle.Poses.Body)==0{return zero,nil,v2input.ErrInvalid}
 encoded,err:=canonical.Marshal(bundle);if err!=nil{return zero,nil,err};if len(encoded)>10<<20{return zero,nil,v2input.ErrInvalid}
 for i,expected:=range entries{
  node,err:=r.Documents.ResolveExportNode(ctx,expected);if err!=nil{return zero,nil,err}
  document,err:=ownExportCompositionDocument(ctx,node,expected);if err!=nil{return zero,nil,err}
  if document.SHA!=documents[i].SHA||!bytes.Equal(document.Body,documents[i].Body){return zero,nil,v2input.ErrInvalid}
 }
 final,err:=r.Source.LookupExportParamsOperation(ctx,entry,append([]byte(nil),body...));if err!=nil{return zero,nil,err}
 if final!=origin{return zero,nil,v2input.ErrInvalid};if err:=ctx.Err();err!=nil{return zero,nil,err}
 return origin,encoded,nil
}

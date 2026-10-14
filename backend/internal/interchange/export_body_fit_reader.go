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

type ExportBodyFitInventoryAuthority interface {
 QualifyExportBodyFitInputInventory(context.Context,v2input.Entry,[]byte,ExportClosureNode)error
}
type exportBodyFitBundle struct {
 Schema int `json:"schema_version"`
 Fit exportCompositionDocument `json:"fit"`
 Before exportCompositionDocument `json:"before"`
 After exportCompositionDocument `json:"after"`
 Alignment exportCompositionDocument `json:"alignment"`
 BeforePose exportCompositionDocument `json:"before_pose"`
 AfterPose exportCompositionDocument `json:"after_pose"`
 BeforeFaces *exportCompositionDocument `json:"before_faces"`
 AfterFaces *exportCompositionDocument `json:"after_faces"`
}
type ExportBodyFitReader struct {
 Inventory ExportBodyFitInventoryAuthority
 Documents ExportClosureNodeResolver
}

// Reads pinned content only. Current causal/model/physical mask qualification
// and native BodyFitOrigin validation remain mandatory separate authorities.
func(r ExportBodyFitReader)ReadExportBodyFitInputs(ctx context.Context,parent v2input.Entry,raw []byte,fit ExportClosureNode)([]byte,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if r.Inventory==nil||r.Documents==nil||parent.Contract!="Params"||!validImportRef(parent.Ref)||len(raw)<2||len(raw)>1<<20||rawSHA(raw)!=parent.Ref.SHA256{return nil,v2input.ErrInvalid}
 body:=append([]byte(nil),raw...)
 owned:=ExportClosureNode{Entry:fit.Entry,Body:append([]byte(nil),fit.Body...)}
 if _,err:=exportBodyFitInputInventory(owned);err!=nil{return nil,err}
 qualify:=func()error{return r.Inventory.QualifyExportBodyFitInputInventory(ctx,parent,append([]byte(nil),body...),ExportClosureNode{Entry:owned.Entry,Body:append([]byte(nil),owned.Body...)})}
 if err:=qualify();err!=nil{return nil,err}
 refs,err:=v2draft.DocumentChildren(owned.Body,"BodyFit",owned.Entry.Ref);if err!=nil{return nil,err}
 bundle:=exportBodyFitBundle{Schema:1,Fit:exportCompositionDocument{SHA:owned.Entry.Ref.SHA256,Body:append(json.RawMessage(nil),owned.Body...)}}
 seen:=map[uuid.UUID]v2input.Entry{parent.Ref.ArtifactID:parent,owned.Entry.Ref.ArtifactID:owned.Entry}
 if parent.Ref.ArtifactID==owned.Entry.Ref.ArtifactID{return nil,v2input.ErrInvalid}
 entries:=[]v2input.Entry{};documents:=[]exportCompositionDocument{}
 remaining:=int64(16<<20)-int64(len(owned.Body))
 for _,ref:=range refs{
  var target *exportCompositionDocument
  switch ref.Path {
  case "Document.params.before":target=&bundle.Before
  case "Document.params.after":target=&bundle.After
  case "Document.params.alignment":target=&bundle.Alignment
  case "Document.params.before_pose":target=&bundle.BeforePose
  case "Document.params.after_pose":target=&bundle.AfterPose
  case "Document.params.before_faces":bundle.BeforeFaces=&exportCompositionDocument{};target=bundle.BeforeFaces
  case "Document.params.after_faces":bundle.AfterFaces=&exportCompositionDocument{};target=bundle.AfterFaces
  default:continue
  }
  expected:=v2input.Entry{Ref:ref.Ref,Contract:ref.Contract}
  if previous,exists:=seen[expected.Ref.ArtifactID];exists&&previous!=expected{return nil,v2input.ErrInvalid}
  seen[expected.Ref.ArtifactID]=expected
  node,err:=r.Documents.ResolveExportNode(ctx,expected);if err!=nil{return nil,err}
  document,err:=ownExportCompositionDocument(ctx,node,expected);if err!=nil{return nil,err}
  if len(document.Body)>2<<20{return nil,v2input.ErrInvalid}
  remaining-=int64(len(document.Body));if remaining<0{return nil,v2input.ErrInvalid}
  *target=document;entries=append(entries,expected);documents=append(documents,document)
 }
 if len(bundle.Before.Body)==0||len(bundle.After.Body)==0||len(bundle.Alignment.Body)==0||len(bundle.BeforePose.Body)==0||len(bundle.AfterPose.Body)==0{return nil,v2input.ErrInvalid}
 encoded,err:=canonical.Marshal(bundle);if err!=nil{return nil,err};if len(encoded)>16<<20{return nil,v2input.ErrInvalid}
 for i,expected:=range entries{
  node,err:=r.Documents.ResolveExportNode(ctx,expected);if err!=nil{return nil,err}
  document,err:=ownExportCompositionDocument(ctx,node,expected);if err!=nil{return nil,err}
  if document.SHA!=documents[i].SHA||!bytes.Equal(document.Body,documents[i].Body){return nil,v2input.ErrInvalid}
 }
 if err:=qualify();err!=nil{return nil,err};if err:=ctx.Err();err!=nil{return nil,err}
 return encoded,nil
}

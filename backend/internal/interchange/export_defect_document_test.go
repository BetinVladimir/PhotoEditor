package interchange

import (
 "context"
 "encoding/json"
 "os"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func TestExportDefectDocumentTypedClosureAndMandatorySemantics(t *testing.T) {
 original,err:=os.ReadFile("../review/testdata/defect-proposal-python.json");if err!=nil {t.Fatal(err)}
 for _,mode:=range []string{"valid","deny","missing","chain","candidate","unknown"} {t.Run(mode,func(t *testing.T){
  var doc map[string]any
  if err:=json.Unmarshal(original,&doc);err!=nil {t.Fatal(err)}
  switch mode {
  case "chain":doc["transform_chain"].(map[string]any)["hash"]=rawSHA([]byte("wrong"))
  case "candidate":doc["candidates"].([]any)[0].(map[string]any)["label"]=6
  case "unknown":doc["extra"]=true
  }
  raw,err:=canonical.Marshal(doc);if err!=nil {t.Fatal(err)}
  entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(raw)},Contract:"DefectCandidates"}
  semantics:=&exportSemanticsStub{deny:mode=="deny"}
  validator:=ExportDependencyDocumentValidator{Semantics:semantics}
  if mode=="missing" {validator.Semantics=nil}
  children,err:=validator.ExportDocumentChildren(context.Background(),entry,raw)
  if mode!="valid" {
   if err==nil {t.Fatal("unqualified proposal accepted")}
   if mode!="deny"&&semantics.calls!=0 {t.Fatal("invalid proposal reached authority")}
   return
  }
  if err!=nil||semantics.calls!=1||len(children)!=3 {t.Fatal("typed proposal closure",children,err)}
  expected:=map[string]string{"image":"WorkingImage","source_pixels":"WorkingPixels","label_map":"DefectLabelMap"}
  for field,contract:=range expected {
   var ref v2input.Ref
   encoded,_:=json.Marshal(doc[field]);if err:=json.Unmarshal(encoded,&ref);err!=nil {t.Fatal(err)}
   found:=false
   for _,child:=range children {if child.Ref==ref&&child.Contract==contract {found=true}}
   if !found {t.Fatal("missing typed child",field)}
  }
  if rawSHA(raw)!=entry.Ref.SHA256 {t.Fatal("authority mutated owned proposal")}
 })}
}

func TestExportDefectReviewRequiresAuthorityAndTypedProposal(t *testing.T) {
 raw,err:=os.ReadFile("../review/testdata/defect-selection-python.json");if err!=nil {t.Fatal(err)}
 normalized,err:=canonical.Bytes(raw);if err!=nil {t.Fatal(err)}
 entry:=v2input.Entry{Ref:v2input.Ref{ArtifactID:uuid.New(),SHA256:rawSHA(normalized)},Contract:"DefectReview"}
 for _,deny:=range []bool{false,true} {
  semantics:=&exportSemanticsStub{deny:deny}
  children,err:=(ExportDependencyDocumentValidator{Semantics:semantics}).ExportDocumentChildren(context.Background(),entry,normalized)
  if deny {if err==nil {t.Fatal("review authority refusal ignored")};continue}
  if err!=nil||len(children)!=3||semantics.calls!=1 {t.Fatal(children,err)}
  expected:=map[string]bool{"WorkingImage":false,"DefectCandidates":false,"DefectLabelMap":false}
  for _,child:=range children {if _,ok:=expected[child.Contract];!ok {t.Fatal("unexpected child",child)};expected[child.Contract]=true}
  for contract,found:=range expected {if !found {t.Fatal("missing dependency",contract)}}
 }
}

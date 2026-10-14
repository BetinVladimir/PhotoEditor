package interchange

import (
 "regexp"
 "strconv"
 "github.com/google/uuid"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)

var resolvedMaskChildPath=regexp.MustCompile(`^Document\.params\.override\.mask_refs\[(0|[1-9][0-9]*)\]\.mask\.(artifact|transform_chain\.steps\[(0|[1-9][0-9]*)\]\.inverse_field)$`)

// Called only after full native ResolvedStyle validation. This is a closed
// reference-role projection, never a replacement for numerical or ACL checks.
func exportResolvedChildren(entry v2input.Entry,raw []byte)([]v2input.Entry,error) {
 if entry.Contract!="ResolvedStyle"||!validImportRef(entry.Ref)||len(raw)<2||len(raw)>8<<20||rawSHA(raw)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 refs,err:=v2draft.OpaqueDocumentChildren(raw,"ResolvedStyle",entry.Ref);if err!=nil{return nil,err}
 children:=[]v2input.Entry{};seen:=map[uuid.UUID]v2input.Entry{}
 for _,ref:=range refs {
  contract:=""
  switch ref.Path {
  case "Document.params.image":contract="WorkingImage"
  case "Document.params.style":contract="StyleV2"
  case "Document.params.stats":contract="Stats20"
  case "Document.absolute_lut.artifact":contract="AbsoluteLUTArray"
  default:
   match:=resolvedMaskChildPath.FindStringSubmatch(ref.Path)
   if match!=nil {
    maskIndex,e:=strconv.Atoi(match[1]);if e!=nil||maskIndex>=128{return nil,v2input.ErrInvalid}
    if match[2]=="artifact"{contract="MaskPixels"}else{
     stepIndex,e:=strconv.Atoi(match[3]);if e!=nil||stepIndex>=128{return nil,v2input.ErrInvalid}
     contract="DenseWarpField"
    }
   }
  }
  if contract==""||!validImportRef(ref.Ref){return nil,v2input.ErrInvalid}
  child:=v2input.Entry{Ref:ref.Ref,Contract:contract}
  if old,ok:=seen[ref.Ref.ArtifactID];ok{if old!=child{return nil,v2input.ErrInvalid};continue}
  seen[ref.Ref.ArtifactID]=child;children=append(children,child)
 }
 return children,nil
}
func validateExportResolvedChildren(entry v2input.Entry,raw []byte,supplied []v2input.Entry)error {
 expected,err:=exportResolvedChildren(entry,raw);if err!=nil{return err}
 if len(expected)!=len(supplied){return v2input.ErrInvalid}
 actual:=map[uuid.UUID]v2input.Entry{}
 for _,child:=range supplied{if _,ok:=actual[child.Ref.ArtifactID];ok{return v2input.ErrInvalid};actual[child.Ref.ArtifactID]=child}
 for _,child:=range expected{if actual[child.Ref.ArtifactID]!=child{return v2input.ErrInvalid}}
 return nil
}

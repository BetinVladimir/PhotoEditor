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
// Semantic qualification must check source geometry, consent/model/runtime pins,
// and operation-specific Params schema. It receives parent-derived context hints
// and must retain them for later child qualification; plain refs lose context.
type ExportDependencySemantics interface{QualifyExportDependency(context.Context,v2input.Entry,[]byte,[]v2draft.LayerRef)error}
type ExportDependencyDocumentValidator struct{Semantics ExportDependencySemantics;Other ExportTypedDocumentValidator}
func(v ExportDependencyDocumentValidator)ExportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if v.Semantics==nil||!validImportRef(entry.Ref)||len(raw)<2||len(raw)>8<<20||rawSHA(raw)!=entry.Ref.SHA256{return nil,v2input.ErrInvalid}
 normalized,err:=canonical.Bytes(raw);if err!=nil||!bytes.Equal(normalized,raw){return nil,v2input.ErrInvalid}
 switch entry.Contract{
 case "LayerStack","Params","CorrectionPlan","CandidateProfile","DescriptorShard","MaskRef","ManualMask","DefectCandidates","DefectReview","DefectProposeReport","Stats20":
 default:
  if v.Other==nil{return nil,v2input.ErrInvalid}
  return v.Other.ExportDocumentChildren(ctx,entry,append([]byte(nil),raw...))
 }
 refs,err:=v2draft.DocumentChildren(raw,entry.Contract,entry.Ref);if err!=nil{return nil,err}
 // Deep-copy context hints as well as refs before invoking installation code.
 encoded,err:=json.Marshal(refs);if err!=nil{return nil,err}
 var hints []v2draft.LayerRef;if err:=json.Unmarshal(encoded,&hints);err!=nil{return nil,err}
 if err:=v.Semantics.QualifyExportDependency(ctx,entry,append([]byte(nil),raw...),hints);err!=nil{return nil,err}
 if err:=ctx.Err();err!=nil{return nil,err}
 children:=[]v2input.Entry{};seen:=map[uuid.UUID]v2input.Entry{}
 for _,ref:=range refs{
  child:=v2input.Entry{Ref:ref.Ref,Contract:ref.Contract}
  if !validImportRef(child.Ref)||child.Contract==""{return nil,v2input.ErrInvalid}
  if old,exists:=seen[child.Ref.ArtifactID];exists{if old!=child{return nil,v2input.ErrInvalid};continue}
  seen[child.Ref.ArtifactID]=child;children=append(children,child)
 }
 return children,ctx.Err()
}

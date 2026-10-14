package interchange

import (
 "context"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

// Scope must be supplied from the frozen server workflow, never decoded from
// an incoming document. This validator covers plan/Params only; downstream
// image and portable recipe validators remain mandatory in the closure walk.
type ExportPlanDocumentValidator struct{Task,Project uuid.UUID;SnapshotDigest string;Plan v2input.Ref}
func(v ExportPlanDocumentValidator)ExportDocumentChildren(ctx context.Context,entry v2input.Entry,raw []byte)([]v2input.Entry,error){
 if err:=ctx.Err();err!=nil{return nil,err}
 if v.Task==uuid.Nil||v.Project==uuid.Nil||!validImportRef(v.Plan)||!validImportRef(entry.Ref)||entry.Ref.SHA256!=rawSHA(raw){return nil,v2input.ErrInvalid}
 if entry.Contract=="Params"{
  if entry.Ref.ArtifactID!=uuid.NewSHA1(v.Task,[]byte("interchange:params:1")){return nil,v2input.ErrInvalid}
  var params exportParamsDocument;if err:=decodeImportDocument(raw,&params);err!=nil{return nil,err}
  if params.Schema!=1||params.Report!=8<<20||params.Plan!=v.Plan||params.Plan.ArtifactID==entry.Ref.ArtifactID{return nil,v2input.ErrInvalid}
  return []v2input.Entry{{Ref:v.Plan,Contract:"InterchangePlan"}},ctx.Err()
 }
 if entry.Contract!="InterchangePlan"||entry.Ref!=v.Plan||v.Plan.ArtifactID!=uuid.NewSHA1(v.Task,[]byte("interchange:plan:1")){return nil,v2input.ErrInvalid}
 var plan importPlanDocument;if err:=decodeImportDocument(raw,&plan);err!=nil{return nil,err}
 if plan.Direction!="export"{return nil,v2input.ErrInvalid}
 for _,item:=range plan.Entries{if item.Binding==nil||item.Recipe==nil{return nil,v2input.ErrInvalid}}
 // Share all canonical/path/metadata/limit and typed-alias rules with import,
 // then strengthen the otherwise opaque content role to encoded image.
 plan.Direction="import"
 checked,err:=canonical.Marshal(plan);if err!=nil{return nil,err}
 children,err:=(ImportPlanDocumentValidator{Project:v.Project,SnapshotDigest:v.SnapshotDigest}).ImportDocumentChildren(ctx,
  v2input.Entry{Ref:v2input.Ref{ArtifactID:entry.Ref.ArtifactID,SHA256:rawSHA(checked)},Contract:"InterchangePlan"},checked)
 if err!=nil{return nil,err}
 for i:=range children{if children[i].Contract=="Artifact"{children[i].Contract="EncodedImage"}}
 return children,ctx.Err()
}

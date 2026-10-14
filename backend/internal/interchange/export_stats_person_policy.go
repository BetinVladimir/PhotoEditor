package interchange

import (
 "context"
 "encoding/json"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/review"
 "photoeditor/backend/internal/semanticproposal"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

type ExportQualifiedPersonReader interface {
 ReadQualifiedPersonEvidence(context.Context,auth.Identity,uuid.UUID,v2input.SessionVersionBinding,v2input.Ref,v2input.Ref,v2qualification.Expected)(semanticproposal.VerifiedPersonMask,error)
}
// Configure only from operator-selected qualification policy and frozen server
// bindings, never report claims or archive-provided profiles.
type ExportStatsPersonPolicy struct {
 reader ExportQualifiedPersonReader
 who auth.Identity
 tenant uuid.UUID
 bindings map[v2input.Ref]v2input.SessionVersionBinding
 report v2input.Ref
 expected []byte
}
func NewExportStatsPersonPolicy(reader ExportQualifiedPersonReader,who auth.Identity,tenantID uuid.UUID,bindings []v2input.SessionVersionBinding,report v2input.Ref,expected v2qualification.Expected)(*ExportStatsPersonPolicy,error){
 if reader==nil||tenantID==uuid.Nil||who.UserID==uuid.Nil||who.SessionID==uuid.Nil||!validImportRef(report)||expected.Operation!="mask.person"||expected.Version!="1"||!expected.LicenseCleared||len(bindings)==0||len(bindings)>30000{return nil,v2input.ErrInvalid}
 for _,digest:=range []string{expected.ModelLockSHA,expected.RuntimeLockSHA,expected.DeviceProfileSHA,expected.DatasetSHA}{if !validImportRef(v2input.Ref{ArtifactID:report.ArtifactID,SHA256:digest}){return nil,v2input.ErrInvalid}}
 if expected.Backend!="cpu"&&expected.Backend!="cuda"&&expected.Backend!="mps"{return nil,v2input.ErrInvalid}
 body,err:=canonical.Marshal(expected);if err!=nil{return nil,err}
 result:=&ExportStatsPersonPolicy{reader:reader,who:who,tenant:tenantID,report:report,expected:body,bindings:map[v2input.Ref]v2input.SessionVersionBinding{}}
 for _,binding:=range bindings{
  if binding.AssetVersionID==uuid.Nil||!validImportRef(binding.Source)||!validImportRef(v2input.Ref{ArtifactID:binding.Source.ArtifactID,SHA256:binding.CanvasHash}){return nil,v2input.ErrInvalid}
  if _,exists:=result.bindings[binding.Source];exists{return nil,v2input.ErrInvalid};result.bindings[binding.Source]=binding
 }
 return result,nil
}
func(s *ExportStatsPersonPolicy)ResolveExportStatsPersonPolicy(ctx context.Context,origin ExportStatsPersonOrigin,source v2input.Ref)(ExportStatsPersonPolicyEvidence,error){
 var zero ExportStatsPersonPolicyEvidence
 if err:=ctx.Err();err!=nil{return zero,err}
 if s==nil||s.reader==nil||!validImportRef(origin.Root)||!validImportRef(source){return zero,v2input.ErrInvalid}
 binding,ok:=s.bindings[source];if !ok{return zero,v2input.ErrInvalid}
 // Every invocation receives owned Expected maps/slices; readers cannot mutate
 // deployment policy shared by the final authority recheck.
 var expected v2qualification.Expected
 if err:=json.Unmarshal(s.expected,&expected);err!=nil{return zero,err}
 mask,err:=s.reader.ReadQualifiedPersonEvidence(ctx,s.who,s.tenant,binding,origin.Root,s.report,expected);if err!=nil{return zero,err}
 if mask.Root!=origin.Root||rawSHA(mask.Body)!=origin.Root.SHA256||mask.Pixels.SHA256!=origin.Mask.SHA256{return zero,v2input.ErrInvalid}
 parsed,err:=review.ParseMaskRef(mask.Body);if err!=nil{return zero,err}
 if parsed.Artifact.ID!=mask.Pixels.ArtifactID||parsed.Artifact.SHA256!=mask.Pixels.SHA256||parsed.ModelLockID==nil||*parsed.ModelLockID!=expected.ModelLockSHA{return zero,v2input.ErrInvalid}
 // Expected above may be mutated by a reader; use immutable configured fields.
 var configured v2qualification.Expected
 if err:=json.Unmarshal(s.expected,&configured);err!=nil{return zero,err}
 if *parsed.ModelLockID!=configured.ModelLockSHA{return zero,v2input.ErrInvalid}
 if err:=ctx.Err();err!=nil{return zero,err}
 return ExportStatsPersonPolicyEvidence{Binding:binding,ModelLock:configured.ModelLockSHA,Backend:configured.Backend,QualificationSHA:s.report.SHA256},nil
}
var _ ExportQualifiedPersonReader=semanticproposal.SQLTransferSourceReader{}
var _ ExportStatsPersonPolicyAuthority=(*ExportStatsPersonPolicy)(nil)
